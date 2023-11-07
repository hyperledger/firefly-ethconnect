// Copyright 2022 Kaleido

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
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/eth"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	log "github.com/sirupsen/logrus"
)

// blockConfirmationManager listens to the blocks on the chain, and attributes confirmations to
// pending events. Once those events meet a threshold they are considered final and
// dispatched to the relevant listener.
type blockConfirmationManager struct {
	ctx                   context.Context
	cancelFunc            func()
	log                   *log.Entry
	filterID              string
	filterStale           bool
	rpc                   eth.RPCClient
	requiredConfirmations int
	pollingInterval       time.Duration
	blockCache            *lru.Cache
	bcmNotifications      chan *bcmNotification
	highestBlockSeen      uint64
	includeInPayload      bool
	pending               map[string]*pendingEvent
	done                  chan struct{}
}

const (
	defaultConfirmations    = 20 /* no perfect answer here for a default, as it's chain and risk assessment dependent */
	defaultBlockCacheSize   = 1000
	defaultEventQueueLength = 100
	defaultPollingInterval  = 1 * time.Second
)

// bcmConfExternal is the YAML/JSON parsable external configuration, with pointers to allow defaults to be applied, and units
type bcmConfExternal struct {
	Enabled                 bool `json:"enabled,omitempty"`
	RequiredConfirmations   *int `json:"requiredConfirmations,omitempty"`
	BlockCacheSize          *int `json:"blockCacheSize,omitempty"`
	BlockPollingIntervalSec *int `json:"pollingIntervalSec,omitempty"`
	EventQueueLength        *int `json:"eventQueueLength,omitempty"`
	IncludeInPayload        bool `json:"includeInPayload,omitempty"`
}

// bcmConfInternal is the parsed config ready to use
type bcmConfInternal struct {
	requiredConfirmations int
	blockCacheSize        int
	pollingInterval       time.Duration
	eventQueueLength      int
	includeInPayload      bool
}

type pendingEvent struct {
	key           string
	added         time.Time
	confirmations []*blockInfo
	event         *eventData
	eventStream   *eventStream
}

type pendingEvents []*pendingEvent

func (pe pendingEvents) Len() int      { return len(pe) }
func (pe pendingEvents) Swap(i, j int) { pe[i], pe[j] = pe[j], pe[i] }
func (pe pendingEvents) Less(i, j int) bool {
	return pe[i].event.blockNumber < pe[j].event.blockNumber ||
		(pe[i].event.blockNumber == pe[j].event.blockNumber && (pe[i].event.transactionIndex < pe[j].event.transactionIndex ||
			(pe[i].event.transactionIndex == pe[j].event.transactionIndex && pe[i].event.logIndex < pe[j].event.logIndex)))
}

type bcmEventType int

const (
	bcmNewLog bcmEventType = iota
	bcmRemovedLog
	bcmStopStream
)

type bcmNotification struct {
	nType       bcmEventType
	event       *eventData
	eventStream *eventStream
	complete    chan struct{} // for bcmStopStream only
}

// blockInfo is the information we cache for a block
type blockInfo struct {
	Number     ethbinding.HexUint `json:"number"`
	Hash       ethbinding.Hash    `json:"hash"`
	ParentHash ethbinding.Hash    `json:"parentHash"`
	Timestamp  ethbinding.HexUint `json:"timestamp"`
}

// MarshalJSON for more consistent JSON output in confirmations array of event
func (bi *blockInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"number":     strconv.FormatUint(uint64(bi.Number), 10),
		"hash":       bi.Hash,
		"parentHash": bi.ParentHash,
		"timestamp":  strconv.FormatUint(uint64(bi.Timestamp), 10),
	})
}

func parseBCMConfig(conf *bcmConfExternal) *bcmConfInternal {
	intConf := &bcmConfInternal{
		requiredConfirmations: defaultConfirmations,
		blockCacheSize:        defaultBlockCacheSize,
		pollingInterval:       defaultPollingInterval,
		eventQueueLength:      defaultEventQueueLength,
	}
	if conf.RequiredConfirmations != nil {
		intConf.requiredConfirmations = *conf.RequiredConfirmations
	}
	if conf.BlockCacheSize != nil {
		intConf.blockCacheSize = *conf.BlockCacheSize
	}
	if conf.BlockPollingIntervalSec != nil {
		intConf.pollingInterval = time.Duration(*conf.BlockPollingIntervalSec) * time.Second
	}
	if conf.EventQueueLength != nil {
		intConf.eventQueueLength = *conf.EventQueueLength
	}
	return intConf
}

func newBlockConfirmationManager(ctx context.Context, rpc eth.RPCClient, conf *bcmConfInternal) (bcm *blockConfirmationManager, err error) {

	bcmCtx, cancelFunc := context.WithCancel(ctx)
	bcm = &blockConfirmationManager{
		ctx:                   bcmCtx,
		cancelFunc:            cancelFunc,
		rpc:                   rpc,
		log:                   log.WithField("job", "blockConfirmationManager"),
		requiredConfirmations: conf.requiredConfirmations,
		pollingInterval:       conf.pollingInterval,
		filterStale:           true,
		bcmNotifications:      make(chan *bcmNotification, conf.eventQueueLength),
		pending:               make(map[string]*pendingEvent),
		includeInPayload:      conf.includeInPayload,
	}
	if bcm.blockCache, err = lru.New(conf.blockCacheSize); err != nil {
		return nil, errors.Errorf(errors.EventStreamsCreateStreamResourceErr, err)
	}
	return bcm, nil
}

func (bcm *blockConfirmationManager) start() {
	bcm.done = make(chan struct{})
	go bcm.confirmationsListener()
}

func (bcm *blockConfirmationManager) stop() {
	bcm.cancelFunc()
	<-bcm.done
}

// Notify is used to notify the confirmation manager of detection of a new logEntry addition or removal
func (bcm *blockConfirmationManager) notify(ev *bcmNotification) {
	bcm.bcmNotifications <- ev
}

func (bcm *blockConfirmationManager) createBlockFilter() error {
	ctx, cancel := context.WithTimeout(bcm.ctx, 30*time.Second)
	defer cancel()
	err := bcm.rpc.CallContext(ctx, &bcm.filterID, "eth_newBlockFilter")
	if err != nil {
		return errors.Errorf(errors.RPCCallReturnedError, "eth_newBlockFilter", err)
	}
	bcm.filterStale = false
	bcm.log.Infof("Created block filter: %s", bcm.filterID)
	return err
}

func (bcm *blockConfirmationManager) pollBlockFilter() ([]*ethbinding.Hash, error) {
	ctx, cancel := context.WithTimeout(bcm.ctx, 30*time.Second)
	defer cancel()
	var blockHashes []*ethbinding.Hash
	if err := bcm.rpc.CallContext(ctx, &blockHashes, "eth_getFilterChanges", bcm.filterID); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "filter not found") {
			bcm.filterStale = true
		}
		return nil, err
	}
	return blockHashes, nil
}

func (bcm *blockConfirmationManager) addToCache(blockInfo *blockInfo) {
	bcm.blockCache.Add(blockInfo.Hash.String(), blockInfo)
	bcm.blockCache.Add(strconv.FormatUint(uint64(blockInfo.Number), 10), blockInfo)
}

func (bcm *blockConfirmationManager) getBlockByHash(blockHash *ethbinding.Hash) (*blockInfo, error) {
	cached, ok := bcm.blockCache.Get(blockHash.String())
	if ok {
		return cached.(*blockInfo), nil
	}

	ctx, cancel := context.WithTimeout(bcm.ctx, 30*time.Second)
	defer cancel()

	var blockInfo *blockInfo
	err := bcm.rpc.CallContext(ctx, &blockInfo, "eth_getBlockByHash", blockHash, false /* only the txn hashes */)
	if err != nil {
		return nil, errors.Errorf(errors.RPCCallReturnedError, "eth_getBlockByHash", err)
	}
	if blockInfo == nil {
		return nil, nil
	}
	bcm.log.Debugf("Downloaded block header by hash: %d / %s parent=%s", blockInfo.Number, blockInfo.Hash, blockInfo.ParentHash)

	bcm.addToCache(blockInfo)
	return blockInfo, nil
}

func (bcm *blockConfirmationManager) getBlockByNumber(blockNumber uint64, expectedParentHash string) (*blockInfo, error) {
	cached, ok := bcm.blockCache.Get(strconv.FormatUint(blockNumber, 10))
	if ok {
		blockInfo := cached.(*blockInfo)
		parentHash := blockInfo.ParentHash.String()
		if parentHash != expectedParentHash {
			// Treat a missing block, or a mismatched block, both as a cache miss and query the node
			bcm.log.Debugf("Block cache miss due to parent hash mismatch: %d / %s parent=%s required=%s ", blockInfo.Number, blockInfo.Hash, parentHash, expectedParentHash)
		} else {
			return blockInfo, nil
		}
	}

	ctx, cancel := context.WithTimeout(bcm.ctx, 30*time.Second)
	defer cancel()
	var blockInfo *blockInfo
	err := bcm.rpc.CallContext(ctx, &blockInfo, "eth_getBlockByNumber", ethbinding.HexUint64(blockNumber), false /* only the txn hashes */)
	if err != nil {
		return nil, errors.Errorf(errors.RPCCallReturnedError, "eth_getBlockByNumber", err)
	}
	if blockInfo == nil {
		return nil, nil
	}
	bcm.log.Debugf("Downloaded block header by number: %d / %s parent=%s", blockInfo.Number, blockInfo.Hash, blockInfo.ParentHash)

	bcm.addToCache(blockInfo)
	return blockInfo, nil
}

func (bcm *blockConfirmationManager) confirmationsListener() {
	defer close(bcm.done)
	pollTimer := time.NewTimer(0)
	notifications := make([]*bcmNotification, 0)
	for {
		popped := false
		for !popped {
			select {
			case <-pollTimer.C:
				popped = true
			case <-bcm.ctx.Done():
				bcm.log.Debugf("Block confirmation listener stopping")
				return
			case notification := <-bcm.bcmNotifications:
				if notification.nType == bcmStopStream {
					// Handle stream notifications immediately
					bcm.streamStopped(notification)
				} else {
					// Defer until after we've got new logs
					notifications = append(notifications, notification)
				}
			}
		}
		pollTimer = time.NewTimer(bcm.pollingInterval)

		// Setup a filter if we're missing one
		if bcm.filterStale {
			if err := bcm.createBlockFilter(); err != nil {
				bcm.log.Errorf("Failed to create block filter: %s", err)
				continue
			}

			if err := bcm.walkChain(); err != nil {
				bcm.log.Errorf("Failed to create walk chain after restoring filter: %s", err)
				continue
			}
		}

		// Do the poll
		blockHashes, err := bcm.pollBlockFilter()
		if err != nil {
			bcm.log.Errorf("Failed to retrieve blocks from filter: %s", err)
			continue
		}

		// Process each new block
		bcm.processBlockHashes(blockHashes)

		// Process any new notifications - we do this at the end, so it can benefit
		// from knowing the latest highestBlockSeen
		if err := bcm.processNotifications(notifications); err != nil {
			bcm.log.Errorf("Failed to process notifications: %s", err)
			continue
		}

		// Clear the notifications array now we've processed them (we keep the slice memory)
		notifications = notifications[:0]

	}

}

func (bcm *blockConfirmationManager) keyForEvent(event *eventData) string {
	return fmt.Sprintf("TX:%s|BLOCK:%s/%s|INDEX:%s|LOG:%s", event.TransactionHash, event.BlockNumber, event.BlockHash, event.TransactionIndex, event.LogIndex)
}

func (bcm *blockConfirmationManager) processNotifications(notifications []*bcmNotification) error {

	for _, n := range notifications {
		switch n.nType {
		case bcmNewLog:
			pending := bcm.addEvent(n.event, n.eventStream)
			if err := bcm.walkChainForEvent(pending); err != nil {
				return err
			}
		case bcmRemovedLog:
			bcm.removeEvent(n.event)
		default:
			// Note that streamStopped is handled in the polling loop directly
			bcm.log.Warnf("Unexpected notification type: %d", n.nType)
		}
	}

	return nil
}

// streamStopped removes all pending work for a given stream, and notifies once done
func (bcm *blockConfirmationManager) streamStopped(notification *bcmNotification) {
	for eventKey, pending := range bcm.pending {
		if pending.eventStream == notification.eventStream {
			delete(bcm.pending, eventKey)
		}
	}
	close(notification.complete)
}

// addEvent is called by the goroutine on receipt of a new event notification
func (bcm *blockConfirmationManager) addEvent(event *eventData, eventStream *eventStream) *pendingEvent {

	// Add the event
	eventKey := bcm.keyForEvent(event)
	pending := &pendingEvent{
		key:           bcm.keyForEvent(event),
		added:         time.Now(),
		confirmations: make([]*blockInfo, 0, bcm.requiredConfirmations),
		event:         event,
		eventStream:   eventStream,
	}
	bcm.pending[eventKey] = pending
	bcm.log.Infof("Added pending event %s", eventKey)
	return pending
}

// removeEvent is called by the goroutine on receipt of a remove event notification
func (bcm *blockConfirmationManager) removeEvent(event *eventData) {
	bcm.log.Infof("Removing stale event %s", bcm.keyForEvent(event))
	delete(bcm.pending, bcm.keyForEvent(event))
}

func (bcm *blockConfirmationManager) processBlockHashes(blockHashes []*ethbinding.Hash) {
	if len(blockHashes) > 0 {
		bcm.log.Debugf("New block notifications %v", blockHashes)
	}

	for _, blockHash := range blockHashes {
		// Get the block header
		block, err := bcm.getBlockByHash(blockHash)
		if err != nil || block == nil {
			bcm.log.Errorf("Failed to retrieve block %s: %v", blockHash, err)
			continue
		}

		// Process the block for confirmations
		bcm.processBlock(block)

		// Update the highest block (used for efficiency in chain walks)
		if uint64(block.Number) > bcm.highestBlockSeen {
			bcm.highestBlockSeen = uint64(block.Number)
		}
	}
}

func (bcm *blockConfirmationManager) processBlock(block *blockInfo) {

	// Go through all the events, adding in the confirmations, and popping any out
	// that have reached their threshold. Then drop the log before logging/processing them.
	parentStr := block.ParentHash.String()
	blockNumber := uint64(block.Number)
	var confirmed pendingEvents
	for eventKey, pending := range bcm.pending {
		// The block might appear at any point in the confirmation list
		expectedParentHash := pending.event.BlockHash
		expectedBlockNumber := pending.event.blockNumber + 1
		for i := 0; i < (len(pending.confirmations) + 1); i++ {
			if parentStr == expectedParentHash && blockNumber == expectedBlockNumber {
				pending.confirmations = append(pending.confirmations[0:i], block)
				bcm.log.Infof("Confirmation %d at block %d / %s event=%s",
					len(pending.confirmations), block.Number, block.Hash.String(), bcm.keyForEvent(pending.event))
				break
			}
			if i < len(pending.confirmations) {
				expectedParentHash = pending.confirmations[i].Hash.String()
			}
			expectedBlockNumber++
		}
		if len(pending.confirmations) >= bcm.requiredConfirmations {
			delete(bcm.pending, eventKey)
			confirmed = append(confirmed, pending)
		}
	}

	// Sort the events to dispatch them in the correct order
	sort.Sort(confirmed)
	for _, c := range confirmed {
		bcm.dispatchConfirmed(c)
	}

}

// dispatchConfirmed drive the event stream for any events that are confirmed, and prunes the state
func (bcm *blockConfirmationManager) dispatchConfirmed(confirmed *pendingEvent) {
	eventKey := bcm.keyForEvent(confirmed.event)
	bcm.log.Infof("Confirmed with %d confirmations event=%s", len(confirmed.confirmations), eventKey)

	if bcm.includeInPayload {
		confirmed.event.Confirmations = confirmed.confirmations
	}
	confirmed.eventStream.handleEvent(confirmed.event)
}

// walkChain goes through each event and sees whether it's valid,
// purging any stale confirmations - or whole events if the filter is invalid
// We do this each time our filter is invalidated
func (bcm *blockConfirmationManager) walkChain() error {

	// Grab a copy of all the pending in order
	pendingEvents := make(pendingEvents, 0, len(bcm.pending))
	for _, pending := range bcm.pending {
		pendingEvents = append(pendingEvents, pending)
	}
	sort.Sort(pendingEvents)

	// Go through them in order - using the cache for efficiency
	for _, pending := range pendingEvents {
		if err := bcm.walkChainForEvent(pending); err != nil {
			return err
		}
	}

	return nil

}

func (bcm *blockConfirmationManager) walkChainForEvent(pending *pendingEvent) (err error) {

	eventKey := bcm.keyForEvent(pending.event)

	blockNumber := pending.event.blockNumber + 1
	expectedParentHash := pending.event.BlockHash
	pending.confirmations = pending.confirmations[:0]
	for {
		// No point in walking past the highest block we've seen via the notifier
		if bcm.highestBlockSeen > 0 && blockNumber > bcm.highestBlockSeen {
			bcm.log.Debugf("Waiting for confirmation after block %d event=%s", bcm.highestBlockSeen, eventKey)
			return nil
		}
		block, err := bcm.getBlockByNumber(blockNumber, expectedParentHash)
		if err != nil {
			return err
		}
		if block == nil {
			bcm.log.Infof("Block %d unavailable walking chain event=%s", blockNumber, eventKey)
			return nil
		}
		candidateParentHash := block.ParentHash.String()
		if candidateParentHash != expectedParentHash {
			bcm.log.Infof("Block mismatch in confirmations: block=%d expected=%s actual=%s confirmations=%d event=%s", blockNumber, expectedParentHash, candidateParentHash, len(pending.confirmations), eventKey)
			return nil
		}
		pending.confirmations = append(pending.confirmations, block)
		if len(pending.confirmations) >= bcm.requiredConfirmations {
			// Ready for dispatch
			bcm.dispatchConfirmed(pending)
			return nil
		}
		blockNumber++
		expectedParentHash = block.Hash.String()
	}

}
