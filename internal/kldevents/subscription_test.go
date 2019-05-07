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
	m := &mockSubMgr{
		stream: newTestStream(),
	}

	i := testSubInfo(event)
	s, err := newSubscription(m, rpc, nil, i)
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)

	s1, err := restoreSubscription(m, rpc, i, &big.Int{})
	assert.NoError(err)

	assert.Equal(s.info.ID, s1.info.ID)
	assert.Equal("*:glastonbury(address,uint256,bool)", s1.info.Name)
	assert.Equal(event.Id(), s.info.Filter.Topics[0][0])
}

func TestCreateWebhookSubWithAddr(t *testing.T) {
	assert := assert.New(t)

	rpc := kldeth.NewMockRPCClientForSync(nil, nil)
	m := &mockSubMgr{stream: newTestStream()}
	event := &kldbind.ABIEvent{
		Name:      "devcon",
		Anonymous: true,
	}

	addr := kldbind.HexToAddress("0x0123456789abcDEF0123456789abCDef01234567")
	s, err := newSubscription(m, rpc, &addr, testSubInfo(event))
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)
	assert.Equal(event.Id(), s.info.Filter.Topics[0][0])
	assert.Equal("0x0123456789abcDEF0123456789abCDef01234567:devcon()", s.info.Name)
}

func TestCreateSubscriptionNoEvent(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{}
	m := &mockSubMgr{stream: newTestStream()}
	_, err := newSubscription(m, nil, nil, testSubInfo(event))
	assert.EqualError(err, "Solidity event name must be specified")
}

func TestCreateSubscriptionNewFilterRPCFailure(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{Name: "party"}
	rpc := kldeth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil)
	m := &mockSubMgr{stream: newTestStream()}
	_, err := newSubscription(m, rpc, nil, testSubInfo(event))
	assert.EqualError(err, "Failed to register filter: pop")
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
	_, err := restoreSubscription(m, nil, testSubInfo(&kldbind.ABIEvent{}), big.NewInt(0))
	assert.EqualError(err, "nope")
}

func TestRestoreSubscriptionNewFilterRPCFailure(t *testing.T) {
	assert := assert.New(t)
	m := &mockSubMgr{}
	rpc := kldeth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil)
	_, err := restoreSubscription(m, rpc, testSubInfo(&kldbind.ABIEvent{}), big.NewInt(0))
	assert.EqualError(err, "Failed to register filter: pop")
}

func TestProcessEventsStaleFilter(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		rpc: kldeth.NewMockRPCClientForSync(fmt.Errorf("filter not found"), nil),
	}
	err := s.processNewEvents()
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
	err := s.processNewEvents()
	// We swallow the error in this case - as we simply couldn't read the event
	assert.NoError(err)
}

func TestUnsubscribe(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		rpc: kldeth.NewMockRPCClientForSync(nil, func(method string, res interface{}, args ...interface{}) {
			*(res.(*string)) = "true"
		}),
	}
	err := s.unsubscribe()
	assert.NoError(err)
	assert.True(s.filterStale)
}

func TestUnsubscribeFail(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{rpc: kldeth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil)}
	err := s.unsubscribe()
	assert.EqualError(err, "pop")
	assert.True(s.filterStale)
}

func TestLoadCheckpointBadJSON(t *testing.T) {
	assert := assert.New(t)
	sm := newTestSubscriptionManager()
	mockKV := newMockKV(nil)
	sm.db = mockKV
	mockKV.kvs[checkpointIDPrefix+"id1"] = []byte(":bad json")
	_, err := sm.loadCheckpoint("id1")
	assert.Error(err)
}
