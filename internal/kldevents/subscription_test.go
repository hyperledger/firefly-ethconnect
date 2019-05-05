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
	"fmt"
	"math/big"
	"testing"

	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldeth"

	"github.com/stretchr/testify/assert"
)

func TestCreateWebhookSub(t *testing.T) {
	assert := assert.New(t)

	rpc := kldeth.NewMockRPCClientForSync(nil, nil)
	kv := newMockKV()
	event := &kldbind.ABIEvent{
		Name: "glastonbury",
		Inputs: []kldbind.ABIArgument{
			kldbind.ABIArgument{
				Name: "field",
				Type: kldbind.ABITypeKnown("address"),
			},
			kldbind.ABIArgument{
				Name: "tents",
				Type: kldbind.ABITypeKnown("uint256"),
			},
			kldbind.ABIArgument{
				Name: "mud",
				Type: kldbind.ABITypeKnown("bool"),
			},
		},
	}
	action := &actionSpec{
		Type: "WebHook",
		Webhook: &webhookAction{
			URL: "http://hello.example.com/world",
		},
	}

	s, err := newSubscription(kv, rpc, nil, event, action)
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)

	s1, err := restoreSubscription(kv, rpc, s.info.ID, &big.Int{})
	assert.NoError(err)

	assert.Equal(s.info.ID, s1.info.ID)
	assert.Equal("*:glastonbury(address,uint256,bool)", s1.info.Name)
	assert.Equal("webhook", s1.info.Action.Type)
	assert.Equal("http://hello.example.com/world", s1.info.Action.Webhook.URL)
	assert.Equal(event.Id(), s.info.Filter.Topics[0][0])
}

func TestCreateWebhookSubWithAddr(t *testing.T) {
	assert := assert.New(t)

	rpc := kldeth.NewMockRPCClientForSync(nil, nil)
	kv := newMockKV()
	event := &kldbind.ABIEvent{
		Name:      "devcon",
		Anonymous: true,
	}
	action := &actionSpec{
		Type: "WebHook",
		Webhook: &webhookAction{
			URL: "http://hello.example.com/world",
		},
	}

	addr := kldbind.HexToAddress("0x0123456789abcDEF0123456789abCDef01234567")
	s, err := newSubscription(kv, rpc, &addr, event, action)
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)
	assert.Equal(event.Id(), s.info.Filter.Topics[0][0])
	assert.Equal("0x0123456789abcDEF0123456789abCDef01234567:devcon()", s.info.Name)
}

func TestRestoreSubscriptionMissing(t *testing.T) {
	assert := assert.New(t)
	kv := newMockKV()
	kv.err = fmt.Errorf("pop")
	_, err := restoreSubscription(kv, nil, "missing", &big.Int{})
	assert.EqualError(err, "Failed to read subscription from key value store: pop")
}

func TestRestoreSubscriptionBad(t *testing.T) {
	assert := assert.New(t)
	kv := newMockKV()
	_, err := restoreSubscription(kv, nil, "bad data", &big.Int{})
	assert.EqualError(err, "Failed to restore subscription from key value store: unexpected end of JSON input")
}

func TestCreateSubscriptionNoEvent(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{}
	_, err := newSubscription(nil, nil, nil, event, nil)
	assert.EqualError(err, "Solidity event name must be specified")
}

func TestCreateSubscriptionPersistFailure(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{Name: "party"}
	action := &actionSpec{
		Type:    "WebHook",
		Webhook: &webhookAction{URL: "http://hello/world"},
	}
	rpc := kldeth.NewMockRPCClientForSync(nil, nil)
	kv := newMockKV()
	kv.err = fmt.Errorf("pop")
	_, err := newSubscription(kv, rpc, nil, event, action)
	assert.EqualError(err, "Failed to store subscription info: pop")
}

func TestCreateSubscriptionMissingWebhookURL(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{Name: "party"}
	action := &actionSpec{
		Type: "WebHook",
	}
	_, err := newSubscription(nil, nil, nil, event, action)
	assert.EqualError(err, "Must specify webhook.url for action type 'webhook'")
}

func TestCreateSubscriptionBadWebhookURL(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{Name: "party"}
	action := &actionSpec{
		Type:    "WebHook",
		Webhook: &webhookAction{URL: ":"},
	}
	_, err := newSubscription(nil, nil, nil, event, action)
	assert.EqualError(err, "Invalid URL in webhook action")
}

func TestCreateSubscriptionUnkonwnType(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{Name: "party"}
	action := &actionSpec{
		Type: "Dance",
	}
	_, err := newSubscription(nil, nil, nil, event, action)
	assert.EqualError(err, "Unknown action type 'Dance'")
}

func TestCreateSubscriptionMissingAction(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{Name: "party"}
	_, err := newSubscription(nil, nil, nil, event, nil)
	assert.EqualError(err, "No action specified")
}

// func TestWithRealData(t *testing.T) {
// 	assert := assert.New(t)
// 	rpc, _ := kldeth.RPCConnect(&kldeth.RPCConnOpts{URL: "https://zzopy4giho:9Bxu7MtK3ZeKsyrxVGAf3oq5p3RKebb7v6Vfa1K-j7A@zzaa005btg-zzedst9mf6-rpc.dev2.photic.io"})
// 	event := &kldbind.ABIEvent{
// 		Name: "Changed",
// 		Inputs: []kldbind.ABIArgument{
// 			kldbind.ABIArgument{
// 				Name: "from",
// 				Type: kldbind.ABITypeKnown("address"),
// 			},
// 			kldbind.ABIArgument{
// 				Name: "i",
// 				Type: kldbind.ABITypeKnown("uint256"),
// 			},
// 			kldbind.ABIArgument{
// 				Name: "s",
// 				Type: kldbind.ABITypeKnown("string"),
// 			},
// 		},
// 	}
// 	action := &actionSpec{
// 		Type:    "WebHook",
// 		Webhook: &webhookAction{URL: "http://hello.example.com/world"},
// 	}
// 	kv := newMockKV()
// 	addr := kldbind.HexToAddress("0xc3d6f4aff1156828d6c452c63286540f5213b70b")
// 	s, err := newSubscription(kv, rpc, &addr, event, action)

// 	assert.NoError(err)
// 	s1, err := restoreSubscription(kv, rpc, s.info.ID, &big.Int{})
// 	assert.NoError(err)
// 	err = s1.processNewEvents()
// 	assert.NoError(err)
// 	err = s1.processNewEvents()
// 	assert.NoError(err)
// 	assert.True(false)
// }
