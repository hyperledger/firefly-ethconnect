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
	// ErrorHandlingSkip processes up to the retry behavior on the stream, then skips to the next event
	ErrorHandlingSkip = "skip"
	// MaxBatchSize is the maximum that a user can specific for their batch size
	MaxBatchSize = 1000
	// DefaultPollingInterval is the default polling interval against the ethereum node
	DefaultPollingInterval = time.Duration(10) * time.Second
	// DefaultExponentialBackoffInitial  is the initial delay for backoff retry
	DefaultExponentialBackoffInitial = time.Duration(1) * time.Second
	// DefaultExponentialBackoffFactor is the factor we use between retries
	DefaultExponentialBackoffFactor = float64(2.0)
)

// StreamInfo configures the stream to perform an action for each event
type StreamInfo struct {
	ID                   string         `json:"id"`
	Path                 string         `json:"path"`
	CreatedISO8601       string         `json:"created"`
	Suspended            bool           `json:"suspended"`
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

type eventStream struct {
	allowPrivateIPs   bool
	spec              *StreamInfo
	eventStream       chan *eventData
	stopped           bool
	dispatcherDone    bool
	processorDone     bool
	pollingInterval   time.Duration
	pollerDone        bool
	inFlight          uint64
	batchCond         *sync.Cond
	batchQueue        *list.List
	batchCount        uint64
	initialRetryDelay time.Duration
	backoffFactor     float64
}

// newEventStream constructor verfies the action is correct, kicks
// off the event batch processor, and blockHWM will be
// initialied to that supplied (zero on initial, or the
// value from the checkpoint)
func newEventStream(allowPrivateIPs bool, spec *StreamInfo) (a *eventStream, err error) {

	if spec == nil {
		return nil, fmt.Errorf("No action specified")
	}

	if spec.BatchSize == 0 {
		spec.BatchSize = 1
	} else if spec.BatchSize > MaxBatchSize {
		spec.BatchSize = MaxBatchSize
	}
	if spec.BlockedRetryDelaySec == 0 {
		spec.BlockedRetryDelaySec = 30
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

	a = &eventStream{
		spec:              spec,
		allowPrivateIPs:   allowPrivateIPs,
		eventStream:       make(chan *eventData),
		batchCond:         sync.NewCond(&sync.Mutex{}),
		batchQueue:        list.New(),
		initialRetryDelay: DefaultExponentialBackoffInitial,
		backoffFactor:     DefaultExponentialBackoffFactor,
		pollingInterval:   DefaultPollingInterval,
	}
	go a.eventPoller()
	go a.batchProcessor()
	go a.batchDispatcher()
	return a, nil
}

// HandleEvent is the entry point for the stream from the event detection logic
func (a *eventStream) handleEvent(event *eventData) {
	// Does nothing more than add it to the batch, to be picked up
	// by the batchDispatcher
	a.eventStream <- event
}

// stop is a lazy stop, that marks a flag for the batch goroutine to pick up
func (a *eventStream) stop() {
	a.batchCond.L.Lock()
	a.stopped = true
	a.eventStream <- nil
	a.batchCond.Broadcast()
	a.batchCond.L.Unlock()
}

// suspend only stops the dispatcher, pushing back as if we're in blockding mode
func (a *eventStream) suspend() {
	a.batchCond.L.Lock()
	a.spec.Suspended = true
	a.batchCond.Broadcast()
	a.batchCond.L.Unlock()
}

// resume resumes the dispatcher
func (a *eventStream) resume() error {
	a.batchCond.L.Lock()
	defer a.batchCond.L.Unlock()
	if !a.processorDone {
		return fmt.Errorf("Event processor is already active. Suspending:%t", a.spec.Suspended)
	}
	a.spec.Suspended = false
	a.processorDone = false
	a.pollerDone = false
	go a.batchProcessor()
	a.batchCond.Broadcast()
	return nil
}

// isBlocked protect us from poling for more events when the stream is blocked.
// Can happen regardless of whether the error handling is
// block or skip. It's just with skip we eventually move onto new messages
// after the retries etc. are complete
func (a *eventStream) isBlocked() bool {
	a.batchCond.L.Lock()
	v := a.inFlight >= a.spec.BatchSize
	a.batchCond.L.Unlock()
	return v
}

// eventPoller checks every few seconds against the ethereum node for any
// new events on the subscriptions that are registered for this stream
func (a *eventStream) eventPoller() {
	defer func() { a.pollerDone = true }()
	for {
		time.Sleep(a.pollingInterval)
	}
}

// batchDispatcher is the goroutine that is alsway available to read new
// events and form them into batches. Because we can't be sure how many
// events we'll be dispatched from blocks before the IsBlocked() feedback
// loop protects us, this logic has to build a list of batches
func (a *eventStream) batchDispatcher() {
	var currentBatch []*eventData
	var batchStart time.Time
	batchTimeout := time.Duration(a.spec.BatchTimeoutMS) * time.Millisecond
	defer func() { a.dispatcherDone = true }()
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
					log.Infof("%s: Event stream stopped while waiting for in-flight batch to fill", a.spec.ID)
					return
				}
				currentBatch = append(currentBatch, event)
			}
		} else {
			// New batch
			event := <-a.eventStream
			if event == nil {
				log.Infof("%s: Event stream stopped", a.spec.ID)
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

func (a *eventStream) suspendOrStop() bool {
	return a.spec.Suspended || a.stopped
}

// batchProcessor picks up batches from the batchDispatcher, and performs the blocking
// actions required to perform the action itself.
// We use a sync.Cond rather than a channel to communicate with this goroutine, as
// it might be blocked for very large periods of time
func (a *eventStream) batchProcessor() {
	defer func() { a.processorDone = true }()
	for {
		// Wait for the next batch, or to be stopped
		a.batchCond.L.Lock()
		for !a.suspendOrStop() && a.batchQueue.Len() == 0 {
			a.batchCond.Wait()
		}
		if a.suspendOrStop() {
			a.batchCond.L.Unlock()
			return
		}
		batchElem := a.batchQueue.Front()
		a.batchCount++
		batchNumber := a.batchCount
		a.batchQueue.Remove(batchElem)
		a.batchCond.L.Unlock()
		// Process the batch - could block for a very long time, particularly if
		// ErrorHandlingBlock is configured.
		a.processBatch(batchNumber, batchElem.Value.([]*eventData))
	}
}

// processBatch is the blocking function to process a batch of events
// It never returns an error, and uses the chosen block/skip ErrorHandling
// behaviour combined with the parameters on the event itself
func (a *eventStream) processBatch(batchNumber uint64, events []*eventData) {
	processed := false
	attempt := 0
	for !a.suspendOrStop() && !processed {
		if attempt > 0 {
			time.Sleep(time.Duration(a.spec.BlockedRetryDelaySec) * time.Second)
		}
		attempt++
		log.Errorf("%s: Batch %d initiated with %d events", a.spec.ID, batchNumber, len(events))
		err := a.performActionWithRetry(batchNumber, events)
		// If we got an error after all of the internal retries within the event
		// handler failed, then the ErrorHandling strategy kicks in
		processed = (err == nil)
		if !processed {
			log.Errorf("%s: Batch %d attempt %d failed. ErrorHandling=%s BlockedRetryDelay=%ds",
				a.spec.ID, batchNumber, attempt, a.spec.ErrorHandling, a.spec.BlockedRetryDelaySec)
			processed = (a.spec.ErrorHandling == ErrorHandlingSkip)
		}
	}
	// If we were suspended, do not ack the batch
	if a.suspendOrStop() {
		return
	}

	// Call all the callbacks on the events, so they can update their high water marks
	// If there are multiple events from one SubID, we call it only once with the
	// last message in the batch
	cbs := make(map[string]*eventData)
	for _, event := range events {
		cbs[event.SubID] = event
	}
	for _, event := range cbs {
		event.batchComplete(event)
	}
	a.batchCond.L.Lock()
	a.inFlight -= uint64(len(events))
	a.batchCond.L.Unlock()
}

// performActionWithRetry performs an action, with exponential backoff retry up
// to a given threshold
func (a *eventStream) performActionWithRetry(batchNumber uint64, events []*eventData) (err error) {
	startTime := time.Now()
	endTime := startTime.Add(time.Duration(a.spec.RetryTimeoutSec) * time.Second)
	delay := a.initialRetryDelay
	var attempt uint64
	complete := false
	for !a.suspendOrStop() && !complete {
		if attempt > 0 {
			log.Infof("%s: Watiting %.2fs before re-attempting batch %d", a.spec.ID, delay.Seconds(), batchNumber)
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * a.backoffFactor)
		}
		attempt++
		switch a.spec.Type {
		case "webhook":
			err = a.attemptWebhookAction(batchNumber, attempt, events)
		}
		complete = err == nil || endTime.Sub(time.Now()) < 0
	}
	return err
}

// isAddressSafe checks for local IPs
func (a *eventStream) isAddressUnsafe(ip *net.IPAddr) bool {
	ip4 := ip.IP.To4()
	return !a.allowPrivateIPs &&
		(ip4[0] == 0 ||
			ip4[0] >= 224 ||
			ip4[0] == 127 ||
			ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] < 32) ||
			(ip4[0] == 192 && ip4[1] == 168))
}

// attemptWebhookAction performs a single attempt of a webhook action
func (a *eventStream) attemptWebhookAction(batchNumber, attempt uint64, events []*eventData) error {
	// We perform DNS resolution explicitly, so that we can exclude private IP address
	// ranges from the target
	u, _ := url.Parse(a.spec.Webhook.URL)
	port := u.Port()
	addr, err := net.ResolveIPAddr("ip4", u.Hostname())
	if err != nil {
		return err
	}
	if a.isAddressUnsafe(addr) {
		err := fmt.Errorf("Cannot send Webhook POST to address: %s", u.Hostname())
		log.Errorf(err.Error())
		return err
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
	log.Infof("%s: POST --> %s (attempt=%d)", a.spec.ID, u.String(), attempt)
	reqBytes, err := json.Marshal(&events)
	if err == nil {
		var res *http.Response
		res, err = netClient.Post(u.String(), "application/json", bytes.NewReader(reqBytes))
		if err == nil {
			ok := (res.StatusCode >= 200 && res.StatusCode < 300)
			log.Infof("%s: POST <-- %s [%d] ok=%t", a.spec.ID, u.String(), res.StatusCode, ok)
			if !ok || log.IsLevelEnabled(log.DebugLevel) {
				bodyBytes, _ := ioutil.ReadAll(res.Body)
				log.Infof("%s: Response body: %s", a.spec.ID, string(bodyBytes))
			}
			if !ok {
				err = fmt.Errorf("%s: Failed with status=%d", a.spec.ID, res.StatusCode)
			}
		}
	}
	if err != nil {
		log.Errorf("%s: POST %s failed (attempt=%d): %s", a.spec.ID, u.String(), attempt, err)
	}
	return err
}
