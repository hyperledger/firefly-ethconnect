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
	"context"
	"math/big"
	"strings"
	"time"

	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	log "github.com/sirupsen/logrus"
)

// persistedFilter is the part of the filter we record to storage
type persistedFilter struct {
	Addresses []kldbind.Address `json:"address,omitempty"`
	Topics    [][]kldbind.Hash  `json:"topics,omitempty"`
}

// ethFilter is the filter structure we send over the wire on eth_newFilter
type ethFilter struct {
	persistedFilter
	FromBlock kldbind.HexBigInt `json:"fromBlock,omitempty"`
	ToBlock   string            `json:"toBlock,omitempty"`
}

// SubscriptionInfo is the persisted data for the subscription
type SubscriptionInfo struct {
	kldmessages.TimeSorted
	ID        string                     `json:"id,omitempty"`
	Path      string                     `json:"path"`
	Name      string                     `json:"name"`
	Stream    string                     `json:"stream"`
	Filter    persistedFilter            `json:"filter"`
	Event     kldbind.MarshalledABIEvent `json:"event"`
	FromBlock string                     `json:"fromBlock,omitempty"`
}

// subscription is the runtime that manages the subscription
type subscription struct {
	info         *SubscriptionInfo
	rpc          kldeth.RPCClient
	lp           *logProcessor
	logName      string
	filterID     kldbind.HexBigInt
	filteredOnce bool
	filterStale  bool
}

func newSubscription(sm subscriptionManager, rpc kldeth.RPCClient, addr *kldbind.Address, i *SubscriptionInfo) (*subscription, error) {
	stream, err := sm.streamByID(i.Stream)
	if err != nil {
		return nil, err
	}
	i.Event.E.RawName = i.Event.E.Name
	s := &subscription{
		info:        i,
		rpc:         rpc,
		lp:          newLogProcessor(i.ID, &i.Event.E, stream),
		logName:     i.ID + ":" + i.Event.E.Sig(),
		filterStale: true,
	}
	f := &i.Filter
	addrStr := "*"
	if addr != nil {
		f.Addresses = []kldbind.Address{*addr}
		addrStr = addr.String()
	}
	event := &i.Event.E
	i.Name = addrStr + ":" + event.Sig()
	if event == nil || event.Name == "" {
		return nil, klderrors.Errorf(klderrors.EventStreamsSubscribeNoEvent)
	}
	// For now we only support filtering on the event type
	f.Topics = [][]kldbind.Hash{[]kldbind.Hash{event.ID()}}
	log.Infof("Created subscription %s %s topic:%s", i.ID, i.Name, event.ID().String())
	return s, nil
}

// GetID returns the ID (for sorting)
func (info *SubscriptionInfo) GetID() string {
	return info.ID
}

func restoreSubscription(sm subscriptionManager, rpc kldeth.RPCClient, i *SubscriptionInfo) (*subscription, error) {
	if i.GetID() == "" {
		return nil, klderrors.Errorf(klderrors.EventStreamsNoID)
	}
	stream, err := sm.streamByID(i.Stream)
	if err != nil {
		return nil, err
	}
	i.Event.E.RawName = i.Event.E.Name
	s := &subscription{
		rpc:         rpc,
		info:        i,
		lp:          newLogProcessor(i.ID, &i.Event.E, stream),
		logName:     i.ID + ":" + i.Event.E.Sig(),
		filterStale: true,
	}
	return s, nil
}

func (s *subscription) setInitialBlockHeight(ctx context.Context) (*big.Int, error) {
	if s.info.FromBlock != "" && s.info.FromBlock != FromBlockLatest {
		var i big.Int
		if _, ok := i.SetString(s.info.FromBlock, 10); !ok {
			return nil, klderrors.Errorf(klderrors.EventStreamsSubscribeBadBlock)
		}
		return &i, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	blockHeight := kldbind.HexBigInt{}
	err := s.rpc.CallContext(ctx, &blockHeight, "eth_blockNumber")
	if err != nil {
		return nil, klderrors.Errorf(klderrors.RPCCallReturnedError, "eth_blockNumber", err)
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

func (s *subscription) restartFilter(ctx context.Context, since *big.Int) error {
	f := &ethFilter{}
	f.persistedFilter = s.info.Filter
	f.FromBlock.ToInt().Set(since)
	f.ToBlock = "latest"
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := s.rpc.CallContext(ctx, &s.filterID, "eth_newFilter", f)
	if err != nil {
		return klderrors.Errorf(klderrors.RPCCallReturnedError, "eth_newFilter", err)
	}
	s.filteredOnce = false
	s.filterStale = false
	log.Infof("%s: created filter from block %s: %s - %+v", s.logName, since.String(), s.filterID.String(), s.info.Filter)
	return err
}

func (s *subscription) processNewEvents(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var logs []*logEntry
	rpcMethod := "eth_getFilterLogs"
	if s.filteredOnce {
		rpcMethod = "eth_getFilterChanges"
	}
	if err := s.rpc.CallContext(ctx, &logs, rpcMethod, s.filterID); err != nil {
		if strings.Contains(err.Error(), "filter not found") {
			s.filterStale = true
		}
		return err
	}
	if len(logs) > 0 {
		// Only log if we received at least one event
		log.Debugf("%s: received %d events (%s)", s.logName, len(logs), rpcMethod)
	}
	for idx, logEntry := range logs {
		if err := s.lp.processLogEntry(s.logName, logEntry, idx); err != nil {
			log.Errorf("Failed to processs event: %s", err)
		}
	}
	s.filteredOnce = true
	return nil
}

func (s *subscription) unsubscribe(ctx context.Context) error {
	var retval string
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s.filterStale = true
	err := s.rpc.CallContext(ctx, &retval, "eth_uninstallFilter", s.filterID)
	log.Infof("%s: Uninstalled filter (retval=%s)", s.logName, retval)
	return err
}

func (s *subscription) blockHWM() big.Int {
	return s.lp.getBlockHWM()
}
