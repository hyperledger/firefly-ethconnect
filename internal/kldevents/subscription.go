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
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
)

const (
	subIDPrefix = "sub-"
)

// persistedFilter is the part of the filter we record to storage
type persistedFilter struct {
	Addresses []kldbind.Address `json:"address,omitempty"`
	Topics    [][]kldbind.Hash  `json:"topics,omitempty"`
}

// ethFilterInitial is the filter structure we send over the wire on eth_newFilter when we first register
type ethFilterInitial struct {
	persistedFilter
	FromBlock string `json:"fromBlock,omitempty"`
	ToBlock   string `json:"toBlock,omitempty"`
}

// ethFilterRestart is the filter structure we send over the wire on eth_newFilter when we restart
type ethFilterRestart struct {
	persistedFilter
	FromBlock kldbind.HexBigInt `json:"fromBlock,omitempty"`
	ToBlock   string            `json:"toBlock,omitempty"`
}

// subscriptionInfo is the persisted data for the subscription
type subscriptionInfo struct {
	ID     string                     `json:"id,omitempty"`
	Name   string                     `json:"name"`
	Action string                     `json:"action"`
	Filter persistedFilter            `json:"filter"`
	Event  kldbind.MarshalledABIEvent `json:"event"`
}

// subscription is the runtime that manages the subscription
type subscription struct {
	info         *subscriptionInfo
	rpc          kldeth.RPCClient
	kv           kvStore
	lp           *logProcessor
	logName      string
	filterID     kldbind.HexBigInt
	filteredOnce bool
}

func newSubscription(sm subscriptionManager, kv kvStore, rpc kldeth.RPCClient, addr *kldbind.Address, event *kldbind.ABIEvent, actionID string) (*subscription, error) {
	action, err := sm.actionByID(actionID)
	if err != nil {
		return nil, err
	}
	i := &subscriptionInfo{
		ID:     subIDPrefix + kldutils.UUIDv4(),
		Event:  kldbind.MarshalledABIEvent{E: *event},
		Action: action.id,
	}
	s := &subscription{
		info:    i,
		kv:      kv,
		rpc:     rpc,
		lp:      newLogProcessor(&i.Event.E, action),
		logName: i.ID + ":" + eventSummary(&i.Event.E),
	}
	f := &i.Filter
	addrStr := "*"
	if addr != nil {
		f.Addresses = []kldbind.Address{*addr}
		addrStr = addr.String()
	}
	i.Name = addrStr + ":" + eventSummary(event)
	if event == nil || event.Name == "" {
		return nil, fmt.Errorf("Solidity event name must be specified")
	}
	// For now we only support filtering on the event type
	f.Topics = [][]kldbind.Hash{[]kldbind.Hash{event.Id()}}
	// Create the filter in Ethereum
	if err := s.initialFilter(); err != nil {
		return nil, fmt.Errorf("Failed to register filter: %s", err)
	}
	// Store the subscription
	infoBytes, _ := json.MarshalIndent(s.info, "", "  ")
	if err := kv.Put(i.ID, infoBytes); err != nil {
		return nil, fmt.Errorf("Failed to store subscription info: %s", err)
	}
	log.Infof("Created subscription %s %s topic:%s", i.ID, i.Name, event.Id().String())
	return s, nil
}

func eventSummary(e *kldbind.ABIEvent) string {
	var sb strings.Builder
	sb.WriteString(e.Name)
	sb.WriteString("(")
	for idx, input := range e.Inputs {
		if idx > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(input.Type.String())
	}
	sb.WriteString(")")
	return sb.String()
}

func restoreSubscription(sm subscriptionManager, kv kvStore, rpc kldeth.RPCClient, key string, since *big.Int) (*subscription, error) {
	infoBytes, err := kv.Get(key)
	if err != nil {
		return nil, fmt.Errorf("Failed to read subscription from key value store: %s", err)
	}
	var i subscriptionInfo
	if err := json.Unmarshal(infoBytes, &i); err != nil {
		log.Errorf("%s: %s", err, string(infoBytes))
		return nil, fmt.Errorf("Failed to restore subscription from key value store: %s", err)
	}
	action, err := sm.actionByID(i.Action)
	if err != nil {
		return nil, err
	}
	s := &subscription{
		kv:      kv,
		rpc:     rpc,
		info:    &i,
		lp:      newLogProcessor(&i.Event.E, action),
		logName: i.ID + ":" + eventSummary(&i.Event.E),
	}
	if err := s.restartFilter(since); err != nil {
		return nil, fmt.Errorf("Failed to register filter: %s", err)
	}
	return s, nil
}

func (s *subscription) initialFilter() error {
	var f ethFilterInitial
	f.persistedFilter = s.info.Filter
	f.FromBlock = "latest"
	f.ToBlock = "latest"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.rpc.CallContext(ctx, &s.filterID, "eth_newFilter", f)
}

func (s *subscription) restartFilter(since *big.Int) error {
	var f ethFilterRestart
	f.persistedFilter = s.info.Filter
	f.FromBlock.ToInt().Set(since)
	f.ToBlock = "latest"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := s.rpc.CallContext(ctx, &s.filterID, "eth_newFilter", f)
	log.Infof("%s: created filter from block %s: %s", s.logName, since.String(), s.filterID.String())
	return err
}

func (s *subscription) processNewEvents() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var logs []*logEntry
	rpcMethod := "eth_getFilterLogs"
	if s.filteredOnce {
		rpcMethod = "eth_getFilterChanges"
	}
	if err := s.rpc.CallContext(ctx, &logs, rpcMethod, s.filterID); err != nil {
		return err
	}
	log.Infof("%s: received %d events (%s)", s.logName, len(logs), rpcMethod)
	for _, logEntry := range logs {
		if err := s.lp.processLogEntry(logEntry); err != nil {
			log.Errorf("Failed to processs event: %s", err)
		}
	}
	s.filteredOnce = true
	return nil
}
