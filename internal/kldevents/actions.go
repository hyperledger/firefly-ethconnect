// Copyright 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldevents

import (
	"bytes"
	"container/list"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// ErrorHandlingBlock blocks the event stream until the handler can accept the event
	ErrorHandlingBlock = "block"
	// ErrorHandlingSkip processes up to the retry behavior on the action, then skips to the next event
	ErrorHandlingSkip = "skip"
	// MaxBatchSize is the maximum that a user can specific for their batch size
	MaxBatchSize = 1000
	// ExponentialBackoffInitial is the initial delay for backoff retry
	ExponentialBackoffInitial = time.Duration(1) * time.Second
	// ExponentialBackoffFactor is the factor we use between retries
	ExponentialBackoffFactor = float64(2.0)
)

// actionSpec configures the action to perform for each event
type actionSpec struct {
	Type                 string         `json:"type,omitempty"`
	BatchSize            uint64         `json:"batchSize,omitempty"`
	BatchTimeoutMS       uint64         `json:"batchTimeoutMS,omitempty"`
	ErrorHandling        string         `json:"errorHandling,omitempty"`
	RetryTimeoutSec      uint64         `json:"retryTimeoutSec,omitempty"`
	BlockedRetryDelaySec uint64         `json:"blockedReryDelaySec,omitempty"`
	Webhook              *webhookAction `json:"webhook,omitempty"`
}

type webhookAction struct {
	URL               string            `json:"url,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	TLSkipHostVerify  bool              `json:"tlsSkipHostVerify,omitempty"`
	RequestTimeoutSec uint32            `json:"requestTimeoutSec,omitempty"`
}

type action struct {
	id              string
	allowPrivateIPs bool
	spec            *actionSpec
	eventStream     chan *eventData
	stopped         bool
	inFlight        uint64
	blockHWM        uint64
	batchCond       *sync.Cond
	batchQueue      *list.List
	batchCount      uint64
}

// newAction constructor verfies the action is correct, kicks
// off the event batch processor, and blockHWM will be
// initialied to that supplied (zero on initial, or the
// value from the checkpoint)
func newAction(id string, allowPrivateIPs bool, spec *actionSpec, blockHWM uint64) (a *action, err error) {

	if spec == nil {
		return nil, fmt.Errorf("No action specified")
	}

	spec.Type = strings.ToLower(spec.Type)
	switch spec.Type {
	case "webhook":
		if spec.Webhook == nil || spec.Webhook.URL == "" {
			return nil, fmt.Errorf("Must specify webhook.url for action type 'webhook'")
		}
		if _, err = url.Parse(spec.Webhook.URL); err != nil {
			return nil, fmt.Errorf("Invalid URL in webhook action")
		}
		if spec.Webhook.RequestTimeoutSec == 0 {
			spec.Webhook.RequestTimeoutSec = 30000
		}
	default:
		return nil, fmt.Errorf("Unknown action type '%s'", spec.Type)
	}

	if strings.ToLower(spec.ErrorHandling) == ErrorHandlingBlock {
		spec.ErrorHandling = ErrorHandlingBlock
	} else {
		spec.ErrorHandling = ErrorHandlingSkip
	}

	if spec.BatchSize == 0 {
		spec.BatchSize = 1
	} else if spec.BatchSize > MaxBatchSize {
		spec.BatchSize = MaxBatchSize
	}
	if spec.BlockedRetryDelaySec == 0 {
		spec.BlockedRetryDelaySec = 30
	}
	a = &action{
		id:          id,
		spec:        spec,
		blockHWM:    blockHWM,
		eventStream: make(chan *eventData),
		batchCond:   sync.NewCond(&sync.Mutex{}),
		batchQueue:  list.New(),
	}
	go a.batchProcessor()
	go a.batchDispatcher()
	return a, nil
}

// stop is a lazy stop, that marks a flag for the batch goroutine to pick up
func (a *action) stop() {
	a.batchCond.L.Lock()
	a.stopped = true
	a.eventStream <- nil
	a.batchCond.Broadcast()
	a.batchCond.L.Unlock()
}

// isBlocked protect us from poling for more events when the action is blocked.
// Can happen regardless of whether the error handling is
// block or skip. It's just with skip we eventually move onto new messages
// after the retries etc. are complete
func (a *action) IsBlocked() bool {
	a.batchCond.L.Lock()
	v := a.inFlight >= a.spec.BatchSize
	a.batchCond.L.Unlock()
	return v
}

// blockHWM returns the current high water mark of blocks we have dispatched
// successfully or skipped up to. This is what gets stored in the checkpoint
// for recovery to minimize the number of redeliveries for our at-least-once
// event delivery semantics
func (a *action) BlockHWM() uint64 {
	a.batchCond.L.Lock()
	v := a.blockHWM
	a.batchCond.L.Unlock()
	return v
}

// HandleEvent is the entry point for the action from the event detection logic
func (a *action) HandleEvent(event *eventData) {
	// Does nothing more than add it to the batch, to be picked up
	// by the batchDispatcher
	a.eventStream <- event
}

// batchDispatcher is the goroutine that is alsway available to read new
// events and form them into batches. Because we can't be sure how many
// events we'll be dispatched from blocks before the IsBlocked() feedback
// loop protects us, this logic has to build a list of batches
func (a *action) batchDispatcher() {
	var currentBatch []*eventData
	var batchStart time.Time
	batchTimeout := time.Duration(a.spec.BatchTimeoutMS) * time.Millisecond
	for {
		// Wait for the next event - if we're in the middle of a batch, we
		// need to cope with a timeout
		timeout := false
		if len(currentBatch) > 0 {
			// Existing batch
			timeLeft := (batchStart.Add(batchTimeout)).Sub(time.Now())
			ctx, cancel := context.WithTimeout(context.Background(), timeLeft)
			select {
			case <-ctx.Done():
				cancel()
				timeout = true
			case event := <-a.eventStream:
				cancel()
				if event == nil {
					log.Infof("%s: Event stream stopped while waiting for in-flight batch to fill", a.id)
					return
				}
				currentBatch = append(currentBatch, event)
			}
		} else {
			// New batch
			event := <-a.eventStream
			if event == nil {
				log.Infof("%s: Event stream stopped", a.id)
				return
			}
			currentBatch = []*eventData{event}
			batchStart = time.Now()
		}
		if timeout || uint64(len(currentBatch)) == a.spec.BatchSize {
			// We are ready to dispatch the batch
			a.batchCond.L.Lock()
			a.inFlight++
			a.batchQueue.PushBack(currentBatch)
			a.batchCond.Broadcast()
			a.batchCond.L.Unlock()
			currentBatch = []*eventData{}
		} else {
			// Just increment in-flight count (batch processor decrements)
			a.batchCond.L.Lock()
			a.inFlight++
			a.batchCond.L.Unlock()
		}
	}
}

// batchProcessor picks up batches from the batchDispatcher, and performs the blocking
// actions required to perform the action itself.
// We use a sync.Cond rather than a channel to communicate with this goroutine, as
// it might be blocked for very large periods of time
func (a *action) batchProcessor() {
	for {
		// Wait for the next batch, or to be stopped
		a.batchCond.L.Lock()
		for !a.stopped && a.batchQueue.Len() == 0 {
			a.batchCond.Wait()
		}
		batchElem := a.batchQueue.Front()
		a.batchCount++
		batchNumber := a.batchCount
		a.batchQueue.Remove(batchElem)
		a.batchCond.L.Unlock()
		if a.stopped {
			return
		}
		// Process the batch - could block for a very long time, particularly if
		// ErrorHandlingBlock is configured.
		a.processBatch(batchNumber, batchElem.Value.([]*eventData))
	}
}

// processBatch is the blocking function to process a batch of events
// It never returns an error, and uses the chosen block/skip ErrorHandling
// behaviour combined with the parameters on the event itself
func (a *action) processBatch(batchNumber uint64, events []*eventData) {
	processed := false
	attempt := 0
	for !processed {
		if attempt > 0 {
			time.Sleep(time.Duration(a.spec.BlockedRetryDelaySec) * time.Second)
		}
		attempt++
		log.Errorf("%s: Batch %d initiated with %d events", a.id, batchNumber, len(events))
		err := a.performActionWithRetry(batchNumber, events)
		// If we got an error after all of the internal retries within the event
		// handler failed, then the ErrorHandling strategy kicks in
		processed = (err == nil)
		if !processed {
			log.Errorf("%s: Batch %d attempt %d failed. ErrorHandling=%s BlockedRetryDelay=%ds",
				a.id, batchNumber, attempt, a.spec.ErrorHandling, a.spec.BlockedRetryDelaySec)
			processed = (a.spec.ErrorHandling == ErrorHandlingSkip)
		}
	}
}

// performActionWithRetry performs an action, with exponential backoff retry up
// to a given threshold
func (a *action) performActionWithRetry(batchNumber uint64, events []*eventData) (err error) {
	startTime := time.Now()
	endTime := startTime.Add(time.Duration(a.spec.RetryTimeoutSec) * time.Second)
	delay := ExponentialBackoffInitial
	var attempt uint64
	complete := false
	for !a.stopped && !complete {
		if attempt > 0 {
			log.Infof("%s: Watiting %.2fs before re-attempting batch %d", a.id, delay.Seconds(), batchNumber)
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * ExponentialBackoffFactor)
		}
		attempt++
		switch a.spec.Type {
		case "webhook":
			err = a.attemptWebhookAction(batchNumber, attempt, events)
		}
		complete = err == nil || endTime.Sub(time.Now()) > 0
	}
	return err
}

// isAddressSafe checks for local IPs
func (a *action) isAddressSafe(ip *net.IPAddr) bool {
	return !a.allowPrivateIPs &&
		(ip.IP[0] == 0 ||
			ip.IP[0] >= 224 ||
			ip.IP[0] == 127 ||
			ip.IP[0] == 10 ||
			(ip.IP[0] == 172 && ip.IP[1] >= 16 && ip.IP[1] < 32) ||
			(ip.IP[0] == 192 && ip.IP[1] == 168))
}

// attemptWebhookAction performs a single attempt of a webhook action
func (a *action) attemptWebhookAction(batchNumber, attempt uint64, events []*eventData) error {
	// We perform DNS resolution explicitly, so that we can exclude private IP address
	// ranges from the target
	u, _ := url.Parse(a.spec.Webhook.URL)
	port := u.Port()
	addr, err := net.ResolveIPAddr("ip4", u.Hostname())
	if err != nil {
		return err
	}
	if !a.isAddressSafe(addr) {
		return fmt.Errorf("Cannot send Webhook POST to address: %s", u.Hostname())
	}
	u.Host = addr.String() + ":" + port
	// Set the timeout
	var transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: a.spec.Webhook.TLSkipHostVerify,
	}
	netClient := &http.Client{
		Timeout:   time.Duration(a.spec.Webhook.RequestTimeoutSec) * time.Second,
		Transport: transport,
	}
	log.Infof("POST --> %s (attempt=%d)", u.String(), attempt)
	reqBytes, err := json.Marshal(&events)
	if err == nil {
		var res *http.Response
		res, err = netClient.Post(u.String(), "application/json", bytes.NewReader(reqBytes))
		if err == nil {
			ok := (res.StatusCode >= 200 || res.StatusCode < 300)
			log.Infof("POST <-- %s [%d] ok=%t", u.String(), res.StatusCode, ok)
			if !ok || log.IsLevelEnabled(log.DebugLevel) {
				bodyBytes, _ := ioutil.ReadAll(res.Body)
				log.Infof("Response body: %s", string(bodyBytes))
			}
			if !ok {
				err = fmt.Errorf("Failed with status=%d", res.StatusCode)
			}
		}
	}
	if err != nil {
		log.Errorf("POST %s failed (attempt=%d): %s", u.String(), attempt, err)
	}
	return err
}
