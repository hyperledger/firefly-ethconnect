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
	log                   *log.Entry
	filterID              ethbinding.HexBigInt
	filterStale           bool
	rpc                   eth.RPCClient
	requiredConfirmations int
	pollingInterval       time.Duration
	blockCache            *lru.Cache
	eventNotifications    chan *eventNotification
	highestBlockSeen      uint64
	includeInPayload      bool
	pending               map[string]*pendingEvent
}

const (
	defaultConfirmations    = 35
	defaultBlockCacheSize   = 1000
	defaultEventQueueLength = 100
	defaultPollingInterval  = 1 * time.Second
)

type blockConfirmationConf struct {
	Enabled                 bool `json:"enabled,omitempty"`
	RequiredConfirmations   *int `json:"requiredConfirmations,omitempty"`
	BlockCacheSize          *int `json:"blockCacheSize,omitempty"`
	BlockPollingIntervalSec *int `json:"pollingIntervalSec,omitempty"`
	EventQueueLength        *int `json:"eventQueueLength,omitempty"`
	IncludeInPayload        bool `json:"includeInPayload,omitempty"`
}

type pendingEvent struct {
	key           string
	added         time.Time
	confirmations []*blockInfo
	blockNumber   uint64
	event         *eventData
	eventStream   *eventStream
}

type pendingEvents []*pendingEvent

func (pe pendingEvents) Len() int      { return len(pe) }
func (pe pendingEvents) Swap(i, j int) { pe[i], pe[j] = pe[j], pe[i] }
func (pe pendingEvents) Less(i, j int) bool {
	return pe[i].event.BlockNumber < pe[j].event.BlockNumber ||
		(pe[i].event.BlockNumber == pe[j].event.BlockNumber && pe[i].event.LogIndex < pe[j].event.LogIndex)
}

type eventNotification struct {
	removed     bool
	event       *eventData
	eventStream *eventStream
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

func newBlockConfirmationManager(ctx context.Context, rpc eth.RPCClient, conf *blockConfirmationConf) (bcm *blockConfirmationManager, err error) {
	requiredConfirmations := defaultConfirmations
	if conf.RequiredConfirmations != nil {
		requiredConfirmations = *conf.RequiredConfirmations
	}
	blockCacheSize := defaultBlockCacheSize
	if conf.BlockCacheSize != nil {
		blockCacheSize = *conf.BlockCacheSize
	}
	pollingInterval := defaultPollingInterval
	if conf.BlockPollingIntervalSec != nil {
		pollingInterval = time.Duration(*conf.BlockPollingIntervalSec) * time.Second
	}
	eventQueueLength := defaultEventQueueLength
	if conf.EventQueueLength != nil {
		eventQueueLength = *conf.EventQueueLength
	}

	bcm = &blockConfirmationManager{
		ctx:                   ctx,
		rpc:                   rpc,
		log:                   log.WithField("job", "blockConfirmationManager"),
		requiredConfirmations: requiredConfirmations,
		pollingInterval:       pollingInterval,
		filterStale:           true,
		eventNotifications:    make(chan *eventNotification, eventQueueLength),
		pending:               make(map[string]*pendingEvent),
		includeInPayload:      conf.IncludeInPayload,
	}
	if bcm.blockCache, err = lru.New(blockCacheSize); err != nil {
		return nil, errors.Errorf(errors.EventStreamsCreateStreamResourceErr, err)
	}
	go bcm.confirmationsListener()
	return bcm, nil
}

// Notify is used to notify the confirmation manager of detection of a new logEntry addition or removal
func (bcm *blockConfirmationManager) notify(ev *eventNotification) {
	bcm.eventNotifications <- ev
}

func (bcm *blockConfirmationManager) createBlockFilter() error {
	ctx, cancel := context.WithTimeout(bcm.ctx, 30*time.Second)
	defer cancel()
	err := bcm.rpc.CallContext(ctx, &bcm.filterID, "eth_newBlockFilter")
	if err != nil {
		return errors.Errorf(errors.RPCCallReturnedError, "eth_newBlockFilter", err)
	}
	bcm.filterStale = false
	bcm.log.Infof("Created block filter: %s", bcm.filterID.String())
	return err
}

func (bcm *blockConfirmationManager) pollBlockFilter() ([]*ethbinding.Hash, error) {
	ctx, cancel := context.WithTimeout(bcm.ctx, 30*time.Second)
	defer cancel()
	var blockHashes []*ethbinding.Hash
	if err := bcm.rpc.CallContext(ctx, &blockHashes, "eth_getFilterChanges", bcm.filterID); err != nil {
		if strings.Contains(err.Error(), "filter not found") {
			bcm.filterStale = true
		}
		return nil, err
	}
	return blockHashes, nil
}

func (bcm *blockConfirmationManager) getBlockByHash(blockHash *ethbinding.Hash) (*blockInfo, error) {
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
	bcm.blockCache.Add(blockInfo.Hash.String(), blockInfo)
	bcm.log.Debugf("Downloded block header: %d / %s (by hash)", blockInfo.Number, blockInfo.Hash)

	// Store it by number in the cache we use for walking the chain
	bcm.blockCache.Add(blockInfo.Number, blockInfo)

	return blockInfo, nil
}

func (bcm *blockConfirmationManager) getBlockByNumber(number uint64) (*blockInfo, error) {
	ctx, cancel := context.WithTimeout(bcm.ctx, 30*time.Second)
	defer cancel()
	var blockInfo *blockInfo
	err := bcm.rpc.CallContext(ctx, &blockInfo, "eth_getBlockByNumber", ethbinding.HexUint64(number), false /* only the txn hashes */)
	if err != nil {
		return nil, errors.Errorf(errors.RPCCallReturnedError, "eth_getBlockByNumber", err)
	}
	if blockInfo == nil {
		return nil, nil
	}
	bcm.log.Debugf("Downloded block header: %d / %s (by number)", blockInfo.Number, blockInfo.Hash)

	// Store it by number in the cache we use for walking the chain
	bcm.blockCache.Add(blockInfo.Number, blockInfo)

	return blockInfo, nil
}

func (bcm *blockConfirmationManager) confirmationsListener() {
	pollTimer := time.NewTimer(0)
	notifications := make([]*eventNotification, 0)
	for {
		timedOut := false
		for !timedOut {
			select {
			case <-pollTimer.C:
				timedOut = true
			case <-bcm.ctx.Done():
				bcm.log.Debugf("Block confirmation listener stopping")
				return
			case ev := <-bcm.eventNotifications:
				notifications = append(notifications, ev)
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
			bcm.log.Errorf("Failed processing notifications: %s", err)
			continue
		}

		// Clear the notifications array now we've processed them (we keep the slice memory)
		notifications = notifications[:0]

	}

}

func (bcm *blockConfirmationManager) keyForEvent(event *eventData) string {
	return fmt.Sprintf("TX:%s|BLOCK:%s/%s|IDX:%s", event.TransactionHash, event.BlockNumber, event.BlockHash, event.LogIndex)
}

func (bcm *blockConfirmationManager) processNotifications(notifications []*eventNotification) error {

	for _, n := range notifications {
		if n.removed {
			bcm.removeEvent(n.event)
		} else {
			pending := bcm.addEvent(n.event, n.eventStream)
			if err := bcm.walkChainForEvent(pending); err != nil {
				return err
			}
		}
	}

	return nil
}

// addEvent is called by the goroutine on receipt of a new event notification
func (bcm *blockConfirmationManager) addEvent(event *eventData, eventStream *eventStream) *pendingEvent {
	// We have settled on a string for block number on the external interface, but need to compare it here
	blockNumber, _ := strconv.ParseUint(event.BlockNumber, 10, 64)

	// Add the event
	eventKey := bcm.keyForEvent(event)
	pending := &pendingEvent{
		key:           bcm.keyForEvent(event),
		added:         time.Now(),
		confirmations: make([]*blockInfo, 0, bcm.requiredConfirmations),
		blockNumber:   blockNumber,
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
	for _, blockHash := range blockHashes {
		// Get the block header
		block, err := bcm.getBlockByHash(blockHash)
		if err != nil || block == nil {
			bcm.log.Errorf("Failed to retrieve block %s: %s", blockHash, err)
			continue
		}

		// Get the block header
		if err = bcm.processBlock(block); err != nil {
			bcm.log.Errorf("Failed to process block %d / %s: %s", block.Number, block.Hash, err)
			continue
		}

		// Update the highest block (used for efficiency in chain walks)
		if uint64(block.Number) > bcm.highestBlockSeen {
			bcm.highestBlockSeen = uint64(block.Number)
		}
	}
}

func (bcm *blockConfirmationManager) processBlock(block *blockInfo) error {

	// Go through all the events, adding in the confirmations, and popping any out
	// that have reached their threshold. Then drop the log before logging/processing them.
	parentStr := block.ParentHash.String()
	blockNumber := uint64(block.Number)
	var confirmed pendingEvents
	for eventKey, pending := range bcm.pending {
		// The block might appear at any point in the confirmation list
		expectedParentHash := pending.event.BlockHash
		expectedBlockNumber := pending.blockNumber + 1
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
	return nil

}

// dispatchConfirmed drive the event stream for any events that are confirmed, and prunes the state
func (bcm *blockConfirmationManager) dispatchConfirmed(confirmed *pendingEvent) {
	eventKey := bcm.keyForEvent(confirmed.event)
	bcm.log.Infof("Confirmed with %d confirmations event=%s", len(confirmed.confirmations), eventKey)
	delete(bcm.pending, eventKey)

	confirmed.event.Confirmations = confirmed.confirmations
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

	blockNumber := pending.blockNumber + 1
	expectedParentHash := pending.event.BlockHash
	pending.confirmations = pending.confirmations[:0]
	for {
		// No point in walking past the highest block we've seen via the notifier
		if bcm.highestBlockSeen > 0 && blockNumber >= bcm.highestBlockSeen {
			bcm.log.Debugf("Waiting for confirmation after block %d event=%s", blockNumber, eventKey)
			return nil
		}
		var block *blockInfo
		cached, ok := bcm.blockCache.Get(blockNumber)
		if ok {
			block = cached.(*blockInfo)
		}
		// Treat a missing block, or a mismatched block, both as a cache miss and query the node
		if block == nil || block.ParentHash.String() != expectedParentHash {
			block, err = bcm.getBlockByNumber(blockNumber)
			if err != nil {
				return err
			}
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
