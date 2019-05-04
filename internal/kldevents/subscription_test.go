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
	"testing"

	"github.com/kaleido-io/ethconnect/internal/kldeth"

	"github.com/stretchr/testify/assert"
)

func TestCreateWebhookSub(t *testing.T) {
	assert := assert.New(t)

	kv := newMockKV()
	event := &kldeth.ABIEvent{
		Name: "glastonbury",
	}
	action := &actionSpec{
		Type: "WebHook",
		Webhook: &webhookAction{
			URL: "http://hello/world",
		},
	}

	s, err := newSubscription(kv, nil, event, action)
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)

	var s1 subscriptionInfo
	infoBytes, _ := kv.Get(s.info.ID)
	err = json.Unmarshal(infoBytes, &s1)
	assert.NoError(err)

	assert.Equal(s.info.ID, s1.ID)
	assert.Equal(*event, *s1.Event)
	assert.Equal("webhook", s1.Action.Type)
	assert.Equal("http://hello/world", s1.Action.Webhook.URL)
	assert.Equal(event.Id(), s.info.Filter.Topics[0][0])
}

func TestCreateWebhookSubWithAddr(t *testing.T) {
	assert := assert.New(t)

	kv := newMockKV()
	event := &kldeth.ABIEvent{
		Name:      "devcon",
		Anonymous: true,
	}
	action := &actionSpec{
		Type: "WebHook",
		Webhook: &webhookAction{
			URL: "http://hello/world",
		},
	}

	addr := kldeth.HexToAddress("0x0123456789abcDEF0123456789abCDef01234567")
	s, err := newSubscription(kv, &addr, event, action)
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)
	assert.Equal(event.Id(), s.info.Filter.Topics[0][0])
	assert.Equal(*event, *s.info.Event)
}

func TestCreateSubscriptionNoEvent(t *testing.T) {
	assert := assert.New(t)
	event := &kldeth.ABIEvent{}
	_, err := newSubscription(nil, nil, event, nil)
	assert.EqualError(err, "Solidity event name must be specified")
}

func TestCreateSubscriptionPersistFailure(t *testing.T) {
	assert := assert.New(t)
	event := &kldeth.ABIEvent{Name: "party"}
	action := &actionSpec{
		Type:    "WebHook",
		Webhook: &webhookAction{URL: "http://hello/world"},
	}
	kv := newMockKV()
	kv.err = fmt.Errorf("pop")
	_, err := newSubscription(kv, nil, event, action)
	assert.EqualError(err, "Failed to store subscription info: pop")
}

func TestCreateSubscriptionMissingWebhookURL(t *testing.T) {
	assert := assert.New(t)
	event := &kldeth.ABIEvent{Name: "party"}
	action := &actionSpec{
		Type: "WebHook",
	}
	_, err := newSubscription(nil, nil, event, action)
	assert.EqualError(err, "Must specify webhook.url for action type 'webhook'")
}

func TestCreateSubscriptionBadWebhookURL(t *testing.T) {
	assert := assert.New(t)
	event := &kldeth.ABIEvent{Name: "party"}
	action := &actionSpec{
		Type:    "WebHook",
		Webhook: &webhookAction{URL: ":"},
	}
	_, err := newSubscription(nil, nil, event, action)
	assert.EqualError(err, "Invalid URL in webhook action")
}

func TestCreateSubscriptionUnkonwnType(t *testing.T) {
	assert := assert.New(t)
	event := &kldeth.ABIEvent{Name: "party"}
	action := &actionSpec{
		Type: "Dance",
	}
	_, err := newSubscription(nil, nil, event, action)
	assert.EqualError(err, "Unknown action type 'Dance'")
}

func TestCreateSubscriptionMissingAction(t *testing.T) {
	assert := assert.New(t)
	event := &kldeth.ABIEvent{Name: "party"}
	_, err := newSubscription(nil, nil, event, nil)
	assert.EqualError(err, "No action specified")
}
