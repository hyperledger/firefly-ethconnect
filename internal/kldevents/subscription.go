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
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
)

const (
	subIDPrefix = "sub-"
)

// persistedFilter is the part of the filter we record to storage
type persistedFilter struct {
	Addresses []kldeth.Address `json:"address,omitempty"`
	Topics    [][]kldeth.Hash  `json:"topics,omitempty"`
}

// ethFilter is the filter structure we send over the wire on eth_newFilter
type ethFilter struct {
	persistedFilter
	FromBlock kldeth.HexBigInt `json:"fromBlock,omitempty"`
	ToBlock   string           `json:"toBlock,omitempty"`
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
	ID     string           `json:"id,omitempty"`
	Filter persistedFilter  `json:"filter"`
	Event  *kldeth.ABIEvent `json:"event"`
	Action *actionSpec      `json:"action"`
}

// subscription is the runtime that manages the subscription
type subscription struct {
	info *subscriptionInfo
	kv   kvStore
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

func newSubscription(kv kvStore, addr *kldeth.Address, event *kldeth.ABIEvent, action *actionSpec) (*subscription, error) {
	i := &subscriptionInfo{
		ID:     subIDPrefix + kldutils.UUIDv4(),
		Event:  event,
		Action: action,
	}
	f := &i.Filter
	s := &subscription{
		info: i,
		kv:   kv,
	}
	if addr != nil {
		f.Addresses = []kldeth.Address{*addr}
	}
	if event == nil || event.Name == "" {
		return nil, fmt.Errorf("Solidity event name must be specified")
	}
	if err := s.verifyAction(action); err != nil {
		return nil, err
	}
	// For now we only support filtering on the event type
	f.Topics = [][]kldeth.Hash{[]kldeth.Hash{event.Id()}}
	// Store the subscription
	infoBytes, _ := json.Marshal(s.info)
	if err := kv.Put(i.ID, infoBytes); err != nil {
		return nil, fmt.Errorf("Failed to store subscription info: %s", err)
	}
	return s, nil
}
