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

package events

import (
	"container/list"
	"context"
	"math/big"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hyperledger/firefly-ethconnect/internal/auth"
	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/messages"
	"github.com/hyperledger/firefly-ethconnect/internal/ws"

	lru "github.com/hashicorp/golang-lru"
	log "github.com/sirupsen/logrus"
)

type DistributionMode string

const (
	DistributionModeBroadcast DistributionMode = "broadcast"
	DistributionModeWLD       DistributionMode = "workloadDistribution"
)

const (
	// FromBlockLatest is the special string that means subscribe from the current block
	FromBlockLatest = "latest"
	// ErrorHandlingBlock blocks the event stream until the handler can accept the event
	ErrorHandlingBlock = "block"
	// ErrorHandlingSkip processes up to the retry behavior on the stream, then skips to the next event
	ErrorHandlingSkip = "skip"
	// MaxBatchSize is the maximum that a user can specific for their batch size
	MaxBatchSize = 1000
	// DefaultExponentialBackoffInitial  is the initial delay for backoff retry
	DefaultExponentialBackoffInitial = time.Duration(1) * time.Second
	// DefaultExponentialBackoffFactor is the factor we use between retries
	DefaultExponentialBackoffFactor = float64(2.0)
	// DefaultTimestampCacheSize is the number of entries we will hold in a LRU cache for block timestamps
	DefaultTimestampCacheSize = 1000
)

// StreamInfo configures the stream to perform an action for each event
type StreamInfo struct {
	messages.TimeSorted
	ID                   string               `json:"id"`
	Name                 string               `json:"name,omitempty"`
	Path                 string               `json:"path"`
	Suspended            bool                 `json:"suspended"`
	Type                 string               `json:"type,omitempty"`
	BatchSize            uint64               `json:"batchSize,omitempty"`
	BatchTimeoutMS       uint64               `json:"batchTimeoutMS,omitempty"`
	ErrorHandling        string               `json:"errorHandling,omitempty"`
	RetryTimeoutSec      uint64               `json:"retryTimeoutSec,omitempty"`
	BlockedRetryDelaySec uint64               `json:"blockedReryDelaySec,omitempty"`
	Webhook              *webhookActionInfo   `json:"webhook,omitempty"`
	WebSocket            *webSocketActionInfo `json:"websocket,omitempty"`
	Timestamps           bool                 `json:"timestamps,omitempty"` // Include block timestamps in the events generated
	TimestampCacheSize   int                  `json:"timestampCacheSize,omitempty"`
	Inputs               bool                 `json:"inputs,omitempty"` // Include input args in the events generated
}

type webhookActionInfo struct {
	URL               string            `json:"url,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	TLSkipHostVerify  bool              `json:"tlsSkipHostVerify,omitempty"`
	RequestTimeoutSec uint32            `json:"requestTimeoutSec,omitempty"`
}

type webSocketActionInfo struct {
	Topic            string           `json:"topic,omitempty"`
	DistributionMode DistributionMode `json:"distributionMode,omitempty"`
}

type eventStream struct {
	sm                  subscriptionManager
	allowPrivateIPs     bool
	spec                *StreamInfo
	eventStream         chan *eventData
	stopped             bool
	pollingInterval     time.Duration
	inFlight            uint64
	batchCond           *sync.Cond
	batchQueue          *list.List
	batchCount          uint64
	initialRetryDelay   time.Duration
	backoffFactor       float64
	updateInProgress    bool
	updateInterrupt     chan struct{} // a zero-sized struct used only for signaling (hand rolled alternative to context)
	blockTimestampCache *lru.Cache
	action              eventStreamAction
	wsChannels          ws.WebSocketChannels

	eventPollerDone     chan struct{}
	batchProcessorDone  chan struct{}
	batchDispatcherDone chan struct{}
}

type eventStreamAction interface {
	attemptBatch(batchNumber, attempt uint64, events []*eventData) error
}

func validateWebSocket(w *webSocketActionInfo) error {
	if w.DistributionMode != "" && w.DistributionMode != DistributionModeBroadcast && w.DistributionMode != DistributionModeWLD {
		return errors.Errorf(errors.EventStreamsInvalidDistributionMode, w.DistributionMode)
	}
	return nil
}

// newEventStream constructor verifies the action is correct, kicks
// off the event batch processor, and blockHWM will be
// initialied to that supplied (zero on initial, or the
// value from the checkpoint)
func newEventStream(sm subscriptionManager, spec *StreamInfo, wsChannels ws.WebSocketChannels) (a *eventStream, err error) {
	if spec == nil || spec.GetID() == "" {
		return nil, errors.Errorf(errors.EventStreamsNoID)
	}

	if spec.BatchSize == 0 {
		spec.BatchSize = 1
	} else if spec.BatchSize > MaxBatchSize {
		spec.BatchSize = MaxBatchSize
	}
	if spec.BatchTimeoutMS == 0 {
		spec.BatchTimeoutMS = 5000
	}
	if spec.BlockedRetryDelaySec == 0 {
		spec.BlockedRetryDelaySec = 30
	}
	if strings.ToLower(spec.ErrorHandling) == ErrorHandlingBlock {
		spec.ErrorHandling = ErrorHandlingBlock
	} else {
		spec.ErrorHandling = ErrorHandlingSkip
	}
	if spec.TimestampCacheSize == 0 {
		spec.TimestampCacheSize = DefaultTimestampCacheSize
	}

	a = &eventStream{
		sm:                sm,
		spec:              spec,
		allowPrivateIPs:   sm.config().WebhooksAllowPrivateIPs,
		eventStream:       make(chan *eventData),
		batchCond:         sync.NewCond(&sync.Mutex{}),
		batchQueue:        list.New(),
		initialRetryDelay: DefaultExponentialBackoffInitial,
		backoffFactor:     DefaultExponentialBackoffFactor,
		pollingInterval:   time.Duration(sm.config().EventPollingIntervalSec) * time.Second,
		wsChannels:        wsChannels,
	}

	if a.blockTimestampCache, err = lru.New(spec.TimestampCacheSize); err != nil {
		return nil, errors.Errorf(errors.EventStreamsCreateStreamResourceErr, err)
	}
	if a.pollingInterval == 0 {
		// Let's us do this from UTs, without exposing it
		a.pollingInterval = 10 * time.Millisecond
	}

	spec.Type = strings.ToLower(spec.Type)
	switch spec.Type {
	case "webhook":
		if a.action, err = newWebhookAction(a, spec.Webhook); err != nil {
			return nil, err
		}
	case "websocket":

		if spec.WebSocket != nil {
			if err := validateWebSocket(spec.WebSocket); err != nil {
				return nil, err
			}
		}

		if a.action, err = newWebSocketAction(a, spec.WebSocket); err != nil {
			return nil, err
		}
	default:
		return nil, errors.Errorf(errors.EventStreamsInvalidActionType, spec.Type)
	}

	a.startEventHandlers(false)
	return a, nil
}

// helper to kick off go routines and any tracking entities
func (a *eventStream) startEventHandlers(resume bool) {
	// create a context that can be used to indicate an update to the eventstream
	a.updateInterrupt = make(chan struct{})
	a.eventPollerDone = make(chan struct{})
	go a.eventPoller()
	a.batchProcessorDone = make(chan struct{})
	go a.batchProcessor()
	// For a pause/resume, the batch dispatcher goroutine is not terminated, hence no need to start it
	if !resume {
		a.batchDispatcherDone = make(chan struct{})
		go a.batchDispatcher()
	}
}

// GetID returns the ID (for sorting)
func (spec *StreamInfo) GetID() string {
	return spec.ID
}

// preUpdateStream sets a flag to indicate updateInProgress and wakes up goroutines waiting on condition variable
func (a *eventStream) preUpdateStream() error {
	a.batchCond.L.Lock()
	if a.updateInProgress {
		a.batchCond.L.Unlock()
		return errors.Errorf(errors.EventStreamsUpdateAlreadyInProgress)
	}
	a.updateInProgress = true
	// close the updateInterrupt channel so that the event handler go routines can be woken up
	close(a.updateInterrupt)
	a.batchCond.Broadcast()
	a.batchCond.L.Unlock()

	a.drainBlockConfirmationManager()

	return nil
}

func (a *eventStream) drainBlockConfirmationManager() {
	bcm := a.sm.confirmationManager()
	if bcm != nil {
		n := &bcmNotification{
			nType:       bcmStopStream,
			eventStream: a,
			complete:    make(chan struct{}),
		}
		bcm.notify(n)
		<-n.complete
	}
}

// postUpdateStream resets flags and kicks off a fresh round of handler go routines
func (a *eventStream) postUpdateStream() {
	a.batchCond.L.Lock()
	a.startEventHandlers(false)
	a.updateInProgress = false
	a.inFlight = 0
	a.batchCond.L.Unlock()
}

func (a *eventStream) checkUpdate(newSpec *StreamInfo) (updatedSpec *StreamInfo, err error) {

	// setUpdated marks that there is a change, and creates a copied object
	specCopy := *a.spec
	setUpdated := func() *StreamInfo {
		if updatedSpec == nil {
			updatedSpec = &specCopy
		}
		return updatedSpec
	}

	if newSpec.Type != "" && newSpec.Type != specCopy.Type {
		return nil, errors.Errorf(errors.EventStreamsCannotUpdateType)
	}
	if specCopy.Type == "webhook" && newSpec.Webhook != nil {
		if newSpec.Webhook.RequestTimeoutSec != 0 && newSpec.Webhook.RequestTimeoutSec != specCopy.Webhook.RequestTimeoutSec {
			setUpdated().Webhook.RequestTimeoutSec = newSpec.Webhook.RequestTimeoutSec
		}
		if newSpec.Webhook.TLSkipHostVerify != specCopy.Webhook.TLSkipHostVerify {
			setUpdated().Webhook.TLSkipHostVerify = newSpec.Webhook.TLSkipHostVerify
		}
		if newSpec.Webhook.URL != "" && newSpec.Webhook.URL != specCopy.Webhook.URL {
			if _, err = url.Parse(newSpec.Webhook.URL); err != nil {
				return nil, errors.Errorf(errors.EventStreamsWebhookInvalidURL)
			}
			setUpdated().Webhook.URL = newSpec.Webhook.URL
		}
		for k, v := range newSpec.Webhook.Headers {
			if specCopy.Webhook.Headers == nil || specCopy.Webhook.Headers[k] != v {
				setUpdated().Webhook.Headers = newSpec.Webhook.Headers
				break
			}
		}
	}
	if specCopy.Type == "websocket" && newSpec.WebSocket != nil {
		if newSpec.WebSocket.Topic != specCopy.WebSocket.Topic {
			setUpdated().WebSocket.Topic = newSpec.WebSocket.Topic
		}
		if newSpec.WebSocket.DistributionMode != specCopy.WebSocket.DistributionMode {
			setUpdated().WebSocket.DistributionMode = newSpec.WebSocket.DistributionMode
		}
		// Validate if we changed it
		if updatedSpec != nil {
			if err := validateWebSocket(newSpec.WebSocket); err != nil {
				return nil, err
			}
		}
	}

	if specCopy.BatchSize != newSpec.BatchSize && newSpec.BatchSize != 0 && newSpec.BatchSize < MaxBatchSize {
		setUpdated().BatchSize = newSpec.BatchSize
	}
	if specCopy.BatchTimeoutMS != newSpec.BatchTimeoutMS && newSpec.BatchTimeoutMS != 0 {
		setUpdated().BatchTimeoutMS = newSpec.BatchTimeoutMS
	}
	if specCopy.BlockedRetryDelaySec != newSpec.BlockedRetryDelaySec && newSpec.BlockedRetryDelaySec != 0 {
		setUpdated().BlockedRetryDelaySec = newSpec.BlockedRetryDelaySec
	}
	if newSpec.ErrorHandling != "" && newSpec.ErrorHandling != specCopy.ErrorHandling {
		if strings.ToLower(newSpec.ErrorHandling) == ErrorHandlingBlock {
			setUpdated().ErrorHandling = ErrorHandlingBlock
		} else {
			setUpdated().ErrorHandling = ErrorHandlingSkip
		}
	}
	if newSpec.Name != "" && specCopy.Name != newSpec.Name {
		setUpdated().Name = newSpec.Name
	}
	if specCopy.Timestamps != newSpec.Timestamps {
		setUpdated().Timestamps = newSpec.Timestamps
	}
	if specCopy.Inputs != newSpec.Inputs {
		setUpdated().Inputs = newSpec.Inputs
	}

	// Return a non-nil object ONLY if there's a change
	return updatedSpec, nil
}

// update modifies an existing eventStream
func (a *eventStream) update(newSpec *StreamInfo) (spec *StreamInfo, err error) {
	log.Infof("%s: Update event stream", a.spec.ID)
	updatedSpec, err := a.checkUpdate(newSpec)
	if err != nil {
		return nil, err
	}
	if updatedSpec == nil {
		log.Infof("%s: No change", a.spec.ID)
		return a.spec, nil
	}

	// set a flag to indicate updateInProgress
	// For any go routines that are Wait() ing on the eventListener, wake them up
	if err := a.preUpdateStream(); err != nil {
		return nil, err
	}
	a.spec = updatedSpec
	// wait for the poked goroutines to finish up
	<-a.eventPollerDone
	<-a.batchProcessorDone
	<-a.batchDispatcherDone
	defer a.postUpdateStream()
	return a.spec, nil
}

// HandleEvent is the entry point for the stream from the event detection logic
func (a *eventStream) handleEvent(event *eventData) {
	// Does nothing more than add it to the batch, to be picked up
	// by the batchDispatcher
	select {
	case a.eventStream <- event:
	case <-a.batchDispatcherDone:
		// If the dispatcher isn't running, then there's no problem - the HWM won't get updated
		log.Infof("Event arrived while event stream shutting down")
	}
}

// stop is a lazy stop, that marks a flag for the batch goroutine to pick up
func (a *eventStream) stop(wait bool) {
	a.batchCond.L.Lock()
	if !a.stopped {
		a.stopped = true
		if a.updateInterrupt != nil {
			close(a.updateInterrupt)
		}
	}
	a.batchCond.Broadcast()
	a.batchCond.L.Unlock()
	if wait {
		<-a.eventPollerDone
		<-a.batchProcessorDone
		<-a.batchDispatcherDone
	}
}

// suspend only stops the dispatcher, pushing back as if we're in blocking mode
func (a *eventStream) suspend() {
	a.batchCond.L.Lock()
	a.spec.Suspended = true
	a.batchCond.Broadcast()
	a.batchCond.L.Unlock()
	a.drainBlockConfirmationManager()
	<-a.eventPollerDone
	<-a.batchProcessorDone
}

func isChannelDone(c chan struct{}) bool {
	var isDone bool
	select {
	case <-c:
		isDone = true
	default:
		isDone = false
	}
	return isDone
}

// resume resumes the dispatcher
func (a *eventStream) resume() error {
	a.batchCond.L.Lock()
	defer a.batchCond.L.Unlock()

	if !isChannelDone(a.batchProcessorDone) || !isChannelDone(a.eventPollerDone) {
		return errors.Errorf(errors.EventStreamsWebhookResumeActive, a.spec.Suspended)
	}
	a.spec.Suspended = false

	a.startEventHandlers(true)
	a.batchCond.Broadcast()
	return nil
}

// isBlocked protect us from polling for more events when the stream is blocked.
// Can happen regardless of whether the error handling is
// block or skip. It's just with skip we eventually move onto new messages
// after the retries etc. are complete
func (a *eventStream) isBlocked() bool {
	a.batchCond.L.Lock()
	inFlight := a.inFlight
	isBlocked := inFlight >= a.spec.BatchSize
	a.batchCond.L.Unlock()
	if isBlocked {
		log.Warnf("%s: Is currently blocked. InFlight=%d BatchSize=%d", a.spec.ID, inFlight, a.spec.BatchSize)
	} else if inFlight > 0 {
		log.Debugf("%s: InFlight=%d BatchSize=%d", a.spec.ID, inFlight, a.spec.BatchSize)
	}
	return isBlocked
}

func (a *eventStream) markAllSubscriptionsStale(ctx context.Context) {
	// Mark all subscriptions stale, so they will re-start from the checkpoint if/when we re-run the poller
	subs := a.sm.subscriptionsForStream(a.spec.ID)
	for _, sub := range subs {
		sub.markFilterStale(ctx, true)
	}
}

// eventPoller checks every few seconds against the ethereum node for any
// new events on the subscriptions that are registered for this stream
func (a *eventStream) eventPoller() {
	defer close(a.eventPollerDone)

	ctx := auth.NewSystemAuthContext()
	var checkpoint map[string]*big.Int
	for !a.suspendOrStop() {
		var err error
		// Load the checkpoint (should only be first time round)
		if checkpoint == nil {
			if checkpoint, err = a.sm.loadCheckpoint(a.spec.ID); err != nil {
				log.Errorf("%s: Failed to load checkpoint: %s", a.spec.ID, err)
			}
		}
		// If we're not blocked, then grab some more events
		subs := a.sm.subscriptionsForStream(a.spec.ID)
		if err == nil && !a.isBlocked() {
			for _, sub := range subs {
				// We do the reset on the event processing thread, to avoid any concurrency issue.
				// It's just an unsubscribe, which clears the resetRequested flag and sets us stale.
				if sub.resetRequested {
					_ = sub.unsubscribe(ctx, false)
					// Clear any checkpoint
					delete(checkpoint, sub.info.ID)
				}
				stale := sub.filterStale
				if stale && !sub.deleting {
					blockHeight, exists := checkpoint[sub.info.ID]
					if !exists || blockHeight.Cmp(big.NewInt(0)) <= 0 {
						blockHeight, err = sub.setInitialBlockHeight(ctx)
					} else if !sub.inCatchupMode() {
						sub.setCheckpointBlockHeight(blockHeight)
					}
					if err == nil {
						err = sub.restartFilter(ctx, blockHeight)
					}
				}
				if err == nil {
					err = sub.processNewEvents(ctx)
				}
				if err != nil {
					log.Errorf("%s: subscription error: %s", a.spec.ID, err)
					err = nil
				}
			}
		}
		// Record a new checkpoint if needed
		if checkpoint != nil {
			changed := false
			for _, sub := range subs {
				i1 := checkpoint[sub.info.ID]
				i2 := sub.blockHWM()

				subChanged := i1 == nil || i1.Cmp(&i2) != 0
				if subChanged {
					log.Debugf("%s: New checkpoint HWM: %s", a.spec.ID, i2.String())
				}
				changed = changed || subChanged
				checkpoint[sub.info.ID] = new(big.Int).Set(&i2)
			}
			if changed {
				if err = a.sm.storeCheckpoint(a.spec.ID, checkpoint); err != nil {
					log.Errorf("%s: Failed to store checkpoint: %s", a.spec.ID, err)
				}
			}
		}
		// the event poller reacts to notification about a stream update, else it starts
		// another round of polling after completion of the pollingInterval
		select {
		case <-a.updateInterrupt:
			// we were notified by the caller about an ongoing update, no need to continue
			log.Infof("%s: Notified of an ongoing stream update, existing event poller", a.spec.ID)
			a.markAllSubscriptionsStale(ctx)
			return
		case <-time.After(a.pollingInterval): //fall through and continue to the next iteration
		}
	}

	a.markAllSubscriptionsStale(ctx)

}

// batchDispatcher is the goroutine that is always available to read new
// events and form them into batches. Because we can't be sure how many
// events we'll be dispatched from blocks before the IsBlocked() feedback
// loop protects us, this logic has to build a list of batches
func (a *eventStream) batchDispatcher() {
	defer close(a.batchDispatcherDone)
	var currentBatch []*eventData
	var batchStart time.Time
	batchTimeout := time.Duration(a.spec.BatchTimeoutMS) * time.Millisecond
	for {
		// Wait for the next event - if we're in the middle of a batch, we
		// need to cope with a timeout
		log.Debugf("%s: Begin batch dispatcher loop, current batch length: %d", a.spec.ID, len(currentBatch))
		timeout := false
		if len(currentBatch) > 0 {
			// Existing batch
			timeLeft := time.Until(batchStart.Add(batchTimeout))
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
			case <-a.updateInterrupt:
				// we were notified by the caller about an ongoing update, cancel the timeout ctx and return
				log.Infof("%s: Notified of an ongoing stream update, will not dispatch batch", a.spec.ID)
				cancel() // cancel the ctx which was started to track timeout
				return
			}
		} else {
			// New batch - react to an update notification or process the next set of events from the stream
			select {
			case <-a.updateInterrupt:
				// we were notified by the caller about an ongoing update, return
				log.Infof("%s: Notified of an ongoing stream update, not waiting for new events", a.spec.ID)
				return
			case event := <-a.eventStream:
				if event == nil {
					log.Infof("%s: Event stream stopped", a.spec.ID)
					return
				}
				currentBatch = []*eventData{event}
				log.Infof("%s: New batch length %d", a.spec.ID, len(currentBatch))
				batchStart = time.Now()
			}
		}
		if timeout || uint64(len(currentBatch)) == a.spec.BatchSize {
			// We are ready to dispatch the batch
			a.batchCond.L.Lock()
			if !timeout {
				a.inFlight++
			}
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
	defer close(a.batchProcessorDone)

	for {
		// Wait for the next batch, or to be stopped
		a.batchCond.L.Lock()
		for !a.suspendOrStop() && a.batchQueue.Len() == 0 {
			if a.updateInProgress {
				a.batchCond.L.Unlock()
				<-a.updateInterrupt
				// we were notified by the caller about an ongoing update, return
				log.Infof("%s: Notified of an ongoing stream update, existing batch processor", a.spec.ID)
				return
			} else {
				a.batchCond.Wait()
			}
		}
		if a.suspendOrStop() {
			log.Infof("%s: Suspended, returning existing batch processor", a.spec.ID)
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
		// Track this as an item in the update wait group
		a.processBatch(batchNumber, batchElem.Value.([]*eventData))
	}
}

// processBatch is the blocking function to process a batch of events
// It never returns an error, and uses the chosen block/skip ErrorHandling
// behaviour combined with the parameters on the event itself
func (a *eventStream) processBatch(batchNumber uint64, events []*eventData) {
	if len(events) == 0 {
		return
	}
	processed := false
	attempt := 0
	for !a.suspendOrStop() && !processed {
		if attempt > 0 {
			select {
			case <-a.updateInterrupt:
				// we were notified by the caller about an ongoing update, no need to continue
				log.Infof("%s: Notified of an ongoing stream update, terminating process batch", a.spec.ID)
				return
			case <-time.After(time.Duration(a.spec.BlockedRetryDelaySec) * time.Second): //fall through and continue
			}
		}
		attempt++
		log.Infof("%s: Batch %d initiated with %d events. FirstBlock=%s LastBlock=%s", a.spec.ID, batchNumber, len(events), events[0].BlockNumber, events[len(events)-1].BlockNumber)
		err := a.performActionWithRetry(batchNumber, events)
		// If we got an error after all of the internal retries within the event
		// handler failed, then the ErrorHandling strategy kicks in
		processed = (err == nil)
		if !processed {
			log.Errorf("%s: Batch %d attempt %d failed. ErrorHandling=%s BlockedRetryDelay=%ds err=%s",
				a.spec.ID, batchNumber, attempt, a.spec.ErrorHandling, a.spec.BlockedRetryDelaySec, err)
			processed = (a.spec.ErrorHandling == ErrorHandlingSkip)
		}
	}

	// decrement the in-flight count if we've processed (wouldn't have occurred if we were suspended or stopped)
	a.batchCond.L.Lock()
	if processed {
		a.inFlight -= uint64(len(events))
	}
	a.batchCond.L.Unlock()

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
			log.Infof("%s: Waiting %.2fs before re-attempting batch %d", a.spec.ID, delay.Seconds(), batchNumber)
			select {
			case <-a.updateInterrupt:
				// we were notified by the caller about an ongoing update, no need to continue
				log.Infof("%s: Notified of an ongoing stream update, terminating perform action for batch number: %d", a.spec.ID, batchNumber)
				return
			case <-time.After(delay): //fall through and continue
			}
			delay = time.Duration(float64(delay) * a.backoffFactor)
		}
		attempt++
		err = a.action.attemptBatch(batchNumber, attempt, events)
		complete = err == nil || time.Until(endTime) < 0
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
