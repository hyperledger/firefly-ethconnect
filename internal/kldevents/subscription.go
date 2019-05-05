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
	"net/url"
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

// actionSpec configures the action to perform for each event
type actionSpec struct {
	Type         string         `json:"type,omitempty"`
	BatchSize    int            `json:"batchSize,omitempty"`
	BatchTimeout int            `json:"batchTimeout,omitempty"`
	Webhook      *webhookAction `json:"webhook,omitempty"`
}

type webhookAction struct {
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// subscriptionInfo is the persisted data for the subscription
type subscriptionInfo struct {
	ID     string                     `json:"id,omitempty"`
	Name   string                     `json:"name"`
	Filter persistedFilter            `json:"filter"`
	Event  kldbind.MarshalledABIEvent `json:"event"`
	Action *actionSpec                `json:"action"`
}

// subscription is the runtime that manages the subscription
type subscription struct {
	info         *subscriptionInfo
	rpc          kldeth.RPCClient
	kv           kvStore
	filterID     kldbind.HexBigInt
	filteredOnce bool
}

func (s *subscription) verifyAction(actionIn *actionSpec) error {
	if actionIn == nil {
		return fmt.Errorf("No action specified")
	}

	// We build a structure with just the one action sub-section matching their lower case
	// action type, regardless of what they submitted
	actionOut := actionSpec{Type: strings.ToLower(actionIn.Type)}
	switch actionOut.Type {
	case "webhook":
		if actionIn.Webhook == nil || actionIn.Webhook.URL == "" {
			return fmt.Errorf("Must specify webhook.url for action type 'webhook'")
		}
		if _, err := url.Parse(actionIn.Webhook.URL); err != nil {
			return fmt.Errorf("Invalid URL in webhook action")
		}
		actionOut.Webhook = actionIn.Webhook
	default:
		return fmt.Errorf("Unknown action type '%s'", actionIn.Type)
	}
	s.info.Action = &actionOut
	return nil
}

func newSubscription(kv kvStore, rpc kldeth.RPCClient, addr *kldbind.Address, event *kldbind.ABIEvent, action *actionSpec) (*subscription, error) {
	i := &subscriptionInfo{
		ID:     subIDPrefix + kldutils.UUIDv4(),
		Event:  kldbind.MarshalledABIEvent{E: *event},
		Action: action,
	}
	s := &subscription{
		info: i,
		kv:   kv,
		rpc:  rpc,
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
	if err := s.verifyAction(action); err != nil {
		return nil, err
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

func restoreSubscription(kv kvStore, rpc kldeth.RPCClient, key string, since *big.Int) (*subscription, error) {
	infoBytes, err := kv.Get(key)
	if err != nil {
		return nil, fmt.Errorf("Failed to read subscription from key value store: %s", err)
	}
	var i subscriptionInfo
	if err := json.Unmarshal(infoBytes, &i); err != nil {
		log.Errorf("%s: %s", err, string(infoBytes))
		return nil, fmt.Errorf("Failed to restore subscription from key value store: %s", err)
	}
	s := &subscription{
		kv:   kv,
		rpc:  rpc,
		info: &i,
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
	log.Infof("%s: created filter from block %s: %s", s.info.ID, since.String(), s.filterID.String())
	return err
}

func (s *subscription) processNewEvents() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var res []logEntry
	rpcMethod := "eth_getFilterLogs"
	if s.filteredOnce {
		rpcMethod = "eth_getFilterChanges"
	}
	if err := s.rpc.CallContext(ctx, &res, rpcMethod, s.filterID); err != nil {
		return err
	}
	s.filteredOnce = true
	return nil
}
