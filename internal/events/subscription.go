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
	"math/big"
	"strings"
	"time"

	"github.com/hyperledger/firefly-ethconnect/internal/contractregistry"
	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/eth"
	"github.com/hyperledger/firefly-ethconnect/internal/ethbind"
	"github.com/hyperledger/firefly-ethconnect/internal/messages"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	log "github.com/sirupsen/logrus"
)

// persistedFilter is the part of the filter we record to storage
type persistedFilter struct {
	Addresses []ethbinding.Address `json:"address,omitempty"`
	Topics    [][]ethbinding.Hash  `json:"topics,omitempty"`
}

// ethFilter is the filter structure we send over the wire on eth_newFilter
type ethFilter struct {
	persistedFilter
	FromBlock ethbinding.HexBigInt `json:"fromBlock,omitempty"`
	ToBlock   string               `json:"toBlock,omitempty"`
}

type SubscriptionCreateDTO struct {
	Name      string                           `json:"name,omitempty"`
	Stream    string                           `json:"stream,omitempty"`
	Event     *ethbinding.ABIElementMarshaling `json:"event,omitempty"`
	Methods   ethbinding.ABIMarshaling         `json:"methods,omitempty"` // an inline set of methods that might emit the event
	FromBlock string                           `json:"fromBlock,omitempty"`
	Address   *ethbinding.Address              `json:"address,omitempty"`
}

type ABIRefOrInline struct {
	contractregistry.ABILocation
	Inline ethbinding.ABIMarshaling `json:"inline,omitempty"`
}

// SubscriptionInfo is the persisted data for the subscription
type SubscriptionInfo struct {
	messages.TimeSorted
	ID           string                           `json:"id,omitempty"`
	Path         string                           `json:"path"`
	Summary      string                           `json:"-"`    // System generated name for the subscription
	Name         string                           `json:"name"` // User provided name for the subscription, set to Summary if missing
	Stream       string                           `json:"stream"`
	Filter       persistedFilter                  `json:"filter"`
	Event        *ethbinding.ABIElementMarshaling `json:"event"`
	FromBlock    string                           `json:"fromBlock,omitempty"`
	ABI          *ABIRefOrInline                  `json:"abi,omitempty"`
	Synchronized bool                             `json:"synchronized"`
}

// subscription is the runtime that manages the subscription
type subscription struct {
	info                *SubscriptionInfo
	rpc                 eth.RPCClient
	cr                  contractregistry.ContractResolver
	lp                  *logProcessor
	logName             string
	filterID            string
	filteredOnce        bool
	filterStale         bool
	deleting            bool
	resetRequested      bool
	catchupBlock        *big.Int
	catchupModeBlockGap int64
	catchupModePageSize int64
}

func newSubscription(sm subscriptionManager, rpc eth.RPCClient, cr contractregistry.ContractResolver, addr *ethbinding.Address, i *SubscriptionInfo) (*subscription, error) {
	stream, err := sm.streamByID(i.Stream)
	if err != nil {
		return nil, err
	}
	event, err := ethbind.API.ABIElementMarshalingToABIEvent(i.Event)
	if err != nil {
		return nil, err
	}
	s := &subscription{
		info:                i,
		rpc:                 rpc,
		cr:                  cr,
		lp:                  newLogProcessor(i.ID, event, stream, sm.confirmationManager()),
		logName:             i.ID + ":" + ethbind.API.ABIEventSignature(event),
		filterStale:         true,
		catchupModeBlockGap: sm.config().CatchupModeBlockGap,
		catchupModePageSize: sm.config().CatchupModePageSize,
	}
	f := &i.Filter
	addrStr := "*"
	if addr != nil {
		f.Addresses = []ethbinding.Address{*addr}
		addrStr = addr.String()
	}
	i.Summary = addrStr + ":" + ethbind.API.ABIEventSignature(event)
	// If a name was not provided by the end user, set it to the system generated summary
	if i.Name == "" {
		log.Debugf("No name provided for subscription, using auto-generated summary:%s", i.Summary)
		i.Name = i.Summary
	}
	if event == nil || event.Name == "" {
		return nil, errors.Errorf(errors.EventStreamsSubscribeNoEvent)
	}
	// For now we only support filtering on the event type
	f.Topics = [][]ethbinding.Hash{{event.ID}}
	log.Infof("Created subscription ID:%s name:%s topic:%s", i.ID, i.Name, event.ID)
	return s, nil
}

// GetID returns the ID (for sorting)
func (info *SubscriptionInfo) GetID() string {
	return info.ID
}

func loadABI(cr contractregistry.ContractResolver, location *ABIRefOrInline) (abi *ethbinding.RuntimeABI, err error) {
	if location == nil {
		return nil, nil
	}
	var abiMarshalling ethbinding.ABIMarshaling
	if location.Inline != nil {
		abiMarshalling = location.Inline
	} else {
		deployMsg, err := cr.GetABI(location.ABILocation, false)
		if err != nil || deployMsg == nil || deployMsg.Contract == nil {
			return nil, err
		}
		abiMarshalling = deployMsg.Contract.ABI
	}
	return ethbind.API.ABIMarshalingToABIRuntime(abiMarshalling)
}

func restoreSubscription(sm subscriptionManager, rpc eth.RPCClient, cr contractregistry.ContractResolver, i *SubscriptionInfo) (*subscription, error) {
	if i.GetID() == "" {
		return nil, errors.Errorf(errors.EventStreamsNoID)
	}
	stream, err := sm.streamByID(i.Stream)
	if err != nil {
		return nil, err
	}
	event, err := ethbind.API.ABIElementMarshalingToABIEvent(i.Event)
	if err != nil {
		return nil, err
	}
	s := &subscription{
		rpc:                 rpc,
		cr:                  cr,
		info:                i,
		lp:                  newLogProcessor(i.ID, event, stream, sm.confirmationManager()),
		logName:             i.ID + ":" + ethbind.API.ABIEventSignature(event),
		filterStale:         true,
		catchupModeBlockGap: sm.config().CatchupModeBlockGap,
		catchupModePageSize: sm.config().CatchupModePageSize,
	}
	return s, nil
}

func (s *subscription) setInitialBlockHeight(ctx context.Context) (*big.Int, error) {
	if s.info.FromBlock != "" && s.info.FromBlock != FromBlockLatest {
		var i big.Int
		if _, ok := i.SetString(s.info.FromBlock, 10); !ok {
			return nil, errors.Errorf(errors.EventStreamsSubscribeBadBlock)
		}
		log.Infof("%s: initial block height for event stream (from block): %s", s.logName, i.String())
		s.lp.initBlockHWM(&i)
		return &i, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	blockHeight := ethbinding.HexBigInt{}
	err := s.rpc.CallContext(ctx, &blockHeight, "eth_blockNumber")
	if err != nil {
		return nil, errors.Errorf(errors.RPCCallReturnedError, "eth_blockNumber", err)
	}
	i := blockHeight.ToInt()
	s.lp.initBlockHWM(i)
	log.Infof("%s: initial block height for event stream (latest block): %s", s.logName, i.String())
	return i, nil
}

func (s *subscription) setCheckpointBlockHeight(i *big.Int) {
	s.lp.initBlockHWM(i)
	log.Infof("%s: checkpoint restored block height for event stream: %s", s.logName, i.String())
}

func (s *subscription) createFilter(ctx context.Context, since *big.Int) error {
	f := &ethFilter{}
	f.persistedFilter = s.info.Filter
	f.FromBlock.ToInt().Set(since)
	f.ToBlock = "latest"
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := s.rpc.CallContext(ctx, &s.filterID, "eth_newFilter", f)
	if err != nil {
		return errors.Errorf(errors.RPCCallReturnedError, "eth_newFilter", err)
	}
	s.catchupBlock = nil // we are not in catchup mode now
	s.info.Synchronized = true
	s.filteredOnce = false
	s.markFilterStale(ctx, false)
	log.Infof("%s: created filter from block %s: %s - %+v", s.logName, since.String(), s.filterID, s.info.Filter)
	return err
}

func (s *subscription) restartFilter(ctx context.Context, checkpoint *big.Int) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	since := checkpoint
	if s.inCatchupMode() {
		// If we're already in catchup mode, we need to look at the current catchupBlock,
		// not the checkpoint.
		since = s.catchupBlock
	}

	blockNumber := ethbinding.HexBigInt{}
	err := s.rpc.CallContext(ctx, &blockNumber, "eth_blockNumber")
	if err != nil {
		return errors.Errorf(errors.RPCCallReturnedError, "eth_blockNumber", err)
	}

	blockGap := new(big.Int).Sub(blockNumber.ToInt(), since).Int64()
	log.Debugf("%s: new filter. Head=%s Position=%s Gap=%d (catchup threshold: %d)", s.logName, blockNumber.ToInt().String(), since.String(), blockGap, s.catchupModeBlockGap)
	if s.catchupModeBlockGap > 0 && blockGap > s.catchupModeBlockGap {
		s.catchupBlock = since // note if we were already in catchup, this does not change anything
		s.info.Synchronized = false
		return nil
	}

	return s.createFilter(ctx, since)
}

func (s *subscription) inCatchupMode() bool {
	return s.catchupBlock != nil
}

// getEventTimestamp adds the block timestamp to the log entry.
// It uses a lru cache (blocknumber, timestamp) in the eventstream to determine the timestamp
// and falls back to querying the node if we don't have timestamp in the cache (at which point it gets
// added to the cache)
func (s *subscription) getEventTimestamp(ctx context.Context, l *logEntry) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// the key in the cache is the block number represented as a string
	blockNumber := l.BlockNumber.String()
	if ts, ok := s.lp.stream.blockTimestampCache.Get(blockNumber); ok {
		// we found the timestamp for the block in our local cache, assert it's type and return, no need to query the chain
		l.Timestamp = ts.(uint64)
		return
	}
	// we didn't find the timestamp in our cache, query the node for the block header where we can find the timestamp
	rpcMethod := "eth_getBlockByNumber"

	var hdr ethbinding.Header
	// 2nd parameter (false) indicates it is sufficient to retrieve only hashes of tx objects
	if err := s.rpc.CallContext(ctx, &hdr, rpcMethod, blockNumber, false); err != nil {
		log.Errorf("Unable to retrieve block[%s] timestamp: %s", blockNumber, err)
		l.Timestamp = 0 // set to 0, we were not able to retrieve the timestamp.
		return
	}
	l.Timestamp = hdr.Time
	s.lp.stream.blockTimestampCache.Add(blockNumber, l.Timestamp)
}

func (s *subscription) getTransactionInputs(ctx context.Context, l *logEntry) {
	abi, err := loadABI(s.cr, s.info.ABI)
	if err != nil || abi == nil {
		return
	}
	info, err := eth.GetTransactionInfo(ctx, s.rpc, l.TransactionHash.String())
	if err != nil {
		log.Infof("%s: error querying transaction info", s.logName)
		return
	}
	if info.From != nil {
		l.InputSigner = info.From.String()
	}
	method, err := abi.MethodById(*info.Input)
	if err != nil {
		log.Infof("%s: could not find matching method", s.logName)
		return
	}
	args, err := eth.DecodeInputs(method, info.Input)
	if err == nil {
		l.InputMethod = method.Name
		l.InputArgs = args
	}
}

func (s *subscription) processCatchupBlocks(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var logs []*logEntry

	f := &ethFilter{}
	f.persistedFilter = s.info.Filter
	f.FromBlock.ToInt().Set(s.catchupBlock)
	endBlock := new(big.Int).Add(s.catchupBlock, big.NewInt(s.catchupModePageSize-1))
	f.ToBlock = "0x" + endBlock.Text(16)

	log.Infof("%s: catchup mode. Blocks %d -> %d", s.logName, s.catchupBlock.Int64(), endBlock.Int64())
	if err := s.rpc.CallContext(ctx, &logs, "eth_getLogs", f); err != nil {
		return errors.Errorf(errors.RPCCallReturnedError, "eth_getLogs", err)
	}
	if len(logs) == 0 {
		// We only want to catch up once - so see if we can update our HWM based on the fact
		// we know these historical blocks are empty.
		s.lp.markNoEvents(endBlock)
	} else {
		s.processLogs(ctx, "eth_getLogs", logs)
	}
	s.catchupBlock = endBlock.Add(endBlock, big.NewInt(1))
	return nil
}

func (s *subscription) processLogs(ctx context.Context, rpcMethod string, logs []*logEntry) {
	if len(logs) > 0 {
		// Only log if we received at least one event
		log.Debugf("%s: received %d events (%s)", s.logName, len(logs), rpcMethod)
	}
	for idx, logEntry := range logs {
		if s.lp.stream.spec.Timestamps {
			s.getEventTimestamp(context.Background(), logEntry)
		}
		if s.lp.stream.spec.Inputs {
			s.getTransactionInputs(ctx, logEntry)
		}
		if err := s.lp.processLogEntry(s.logName, logEntry, idx); err != nil {
			log.Errorf("Failed to process event: %s", err)
		}
	}
}

func (s *subscription) processNewEvents(ctx context.Context) error {
	if s.catchupBlock != nil {
		return s.processCatchupBlocks(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var logs []*logEntry
	rpcMethod := "eth_getFilterLogs"
	if s.filteredOnce {
		rpcMethod = "eth_getFilterChanges"
	}
	if err := s.rpc.CallContext(ctx, &logs, rpcMethod, s.filterID); err != nil {
		if strings.Contains(err.Error(), "filter not found") {
			s.markFilterStale(ctx, true)
		}
		return err
	}
	s.processLogs(ctx, rpcMethod, logs)
	s.filteredOnce = true
	return nil
}

func (s *subscription) unsubscribe(ctx context.Context, deleting bool) (err error) {
	log.Infof("%s: Unsubscribing existing filter (deleting=%t)", s.logName, deleting)
	s.deleting = deleting
	s.resetRequested = false
	s.markFilterStale(ctx, true)
	return err
}

func (s *subscription) requestReset() {
	// We simply set a flag, which is picked up by the event stream thread on the next polling cycle
	// and results in an unsubscribe/subscribe cycle.
	log.Infof("%s: Requested reset from block '%s'", s.logName, s.info.FromBlock)
	s.resetRequested = true
}

func (s *subscription) blockHWM() big.Int {
	return s.lp.getBlockHWM()
}

func (s *subscription) markFilterStale(ctx context.Context, newFilterStale bool) {
	log.Debugf("%s: Marking filter stale=%t, current sub filter stale=%t", s.logName, newFilterStale, s.filterStale)
	// If unsubscribe is called multiple times, we might not have a filter
	if newFilterStale && !s.filterStale {
		var retval bool
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		err := s.rpc.CallContext(ctx, &retval, "eth_uninstallFilter", s.filterID)
		// We treat error as informational here - the filter might already not be valid (if the node restarted)
		log.Infof("%s: Uninstalled filter. ok=%t (%s)", s.logName, retval, err)
		// Clear any catchup mode state. We will restart from the last checkpoint
		s.catchupBlock = nil
		s.info.Synchronized = false
	}
	s.filterStale = newFilterStale
}
