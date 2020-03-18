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
	"fmt"
	"math/big"
	"testing"

	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldkvstore"

	"github.com/stretchr/testify/assert"
)

type mockSubMgr struct {
	stream        *eventStream
	subscription  *subscription
	err           error
	subscriptions []*subscription
}

func (m *mockSubMgr) config() *SubscriptionManagerConf {
	return &SubscriptionManagerConf{}
}

func (m *mockSubMgr) streamByID(string) (*eventStream, error) {
	return m.stream, m.err
}

func (m *mockSubMgr) subscriptionByID(string) (*subscription, error) {
	return m.subscription, m.err
}

func (m *mockSubMgr) subscriptionsForStream(string) []*subscription {
	return m.subscriptions
}

func (m *mockSubMgr) loadCheckpoint(string) (map[string]*big.Int, error) { return nil, nil }

func (m *mockSubMgr) storeCheckpoint(string, map[string]*big.Int) error { return nil }

func newTestStream() *eventStream {
	a, _ := newEventStream(newTestSubscriptionManager(), &StreamInfo{
		ID:   "123",
		Type: "WebHook",
		Webhook: &webhookAction{
			URL: "http://hello.example.com/world",
		},
	})
	return a
}

func testSubInfo(event *kldbind.ABIEvent) *SubscriptionInfo {
	return &SubscriptionInfo{ID: "test", Stream: "streamID", Event: kldbind.MarshalledABIEvent{E: *event}}
}

func TestCreateWebhookSub(t *testing.T) {
	assert := assert.New(t)

	rpc := kldeth.NewMockRPCClientForSync(nil, nil)
	event := &kldbind.ABIEvent{
		Name:    "glastonbury",
		RawName: "glastonbury",
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
	m := &mockSubMgr{
		stream: newTestStream(),
	}

	i := testSubInfo(event)
	s, err := newSubscription(m, rpc, nil, i)
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)

	s1, err := restoreSubscription(m, rpc, i)
	assert.NoError(err)

	assert.Equal(s.info.ID, s1.info.ID)
	assert.Equal("*:glastonbury(address,uint256,bool)", s1.info.Name)
	assert.Equal(event.ID(), s.info.Filter.Topics[0][0])
}

func TestCreateWebhookSubWithAddr(t *testing.T) {
	assert := assert.New(t)

	rpc := kldeth.NewMockRPCClientForSync(nil, nil)
	m := &mockSubMgr{stream: newTestStream()}
	event := &kldbind.ABIEvent{
		Name:      "devcon",
		RawName:   "devcon",
		Anonymous: true,
	}

	addr := kldbind.HexToAddress("0x0123456789abcDEF0123456789abCDef01234567")
	s, err := newSubscription(m, rpc, &addr, testSubInfo(event))
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)
	assert.Equal(event.ID(), s.info.Filter.Topics[0][0])
	assert.Equal("0x0123456789abcDEF0123456789abCDef01234567:devcon()", s.info.Name)
}

func TestCreateSubscriptionNoEvent(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{}
	m := &mockSubMgr{stream: newTestStream()}
	_, err := newSubscription(m, nil, nil, testSubInfo(event))
	assert.EqualError(err, "Solidity event name must be specified")
}

func TestCreateSubscriptionMissingAction(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{Name: "party"}
	m := &mockSubMgr{err: fmt.Errorf("nope")}
	_, err := newSubscription(m, nil, nil, testSubInfo(event))
	assert.EqualError(err, "nope")
}

func TestRestoreSubscriptionMissingAction(t *testing.T) {
	assert := assert.New(t)
	m := &mockSubMgr{err: fmt.Errorf("nope")}
	_, err := restoreSubscription(m, nil, testSubInfo(&kldbind.ABIEvent{}))
	assert.EqualError(err, "nope")
}

func TestProcessEventsStaleFilter(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		rpc: kldeth.NewMockRPCClientForSync(fmt.Errorf("filter not found"), nil),
	}
	err := s.processNewEvents(context.Background())
	assert.EqualError(err, "filter not found")
	assert.True(s.filterStale)
}

func TestProcessEventsCannotProcess(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		rpc: kldeth.NewMockRPCClientForSync(nil, func(method string, res interface{}, args ...interface{}) {
			les := res.(*[]*logEntry)
			*les = append(*les, &logEntry{
				Data: "0x no hex here sorry",
			})
		}),
		lp: newLogProcessor("", &kldbind.ABIEvent{}, newTestStream()),
	}
	err := s.processNewEvents(context.Background())
	// We swallow the error in this case - as we simply couldn't read the event
	assert.NoError(err)
}

func TestInitialFilterFail(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		info: &SubscriptionInfo{},
		rpc:  kldeth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil),
	}
	_, err := s.setInitialBlockHeight(context.Background())
	assert.EqualError(err, "eth_blockNumber returned: pop")
}

func TestInitialFilterBadInitialBlock(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		info: &SubscriptionInfo{
			FromBlock: "!integer",
		},
		rpc: kldeth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil),
	}
	_, err := s.setInitialBlockHeight(context.Background())
	assert.EqualError(err, "FromBlock cannot be parsed as a BigInt")
}

func TestInitialFilterCustomInitialBlock(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		info: &SubscriptionInfo{
			FromBlock: "12345",
		},
	}
	res, err := s.setInitialBlockHeight(context.Background())
	assert.NoError(err)
	assert.Equal("12345", res.Text(10))
}

func TestRestartFilterFail(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		info: &SubscriptionInfo{},
		rpc:  kldeth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil),
	}
	err := s.restartFilter(context.Background(), big.NewInt(0))
	assert.EqualError(err, "eth_newFilter returned: pop")
}

func TestUnsubscribe(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		rpc: kldeth.NewMockRPCClientForSync(nil, func(method string, res interface{}, args ...interface{}) {
			*(res.(*string)) = "true"
		}),
	}
	err := s.unsubscribe(context.Background())
	assert.NoError(err)
	assert.True(s.filterStale)
}

func TestUnsubscribeFail(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{rpc: kldeth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil)}
	err := s.unsubscribe(context.Background())
	assert.EqualError(err, "pop")
	assert.True(s.filterStale)
}

func TestLoadCheckpointBadJSON(t *testing.T) {
	assert := assert.New(t)
	sm := newTestSubscriptionManager()
	mockKV := kldkvstore.NewMockKV(nil)
	sm.db = mockKV
	mockKV.KVS[checkpointIDPrefix+"id1"] = []byte(":bad json")
	_, err := sm.loadCheckpoint("id1")
	assert.Error(err)
}
