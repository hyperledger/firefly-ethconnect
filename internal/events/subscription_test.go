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
	"fmt"
	"math/big"
	"testing"

	"github.com/hyperledger-labs/firefly-ethconnect/internal/eth"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/ethbind"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/kvstore"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"

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
		Webhook: &webhookActionInfo{
			URL: "http://hello.example.com/world",
		},
	}, nil)
	return a
}

func testSubInfo(event *ethbinding.ABIElementMarshaling) *SubscriptionInfo {
	return &SubscriptionInfo{ID: "test", Stream: "streamID", Event: event}
}

func TestCreateWebhookSub(t *testing.T) {
	assert := assert.New(t)

	rpc := eth.NewMockRPCClientForSync(nil, nil)
	event := &ethbinding.ABIElementMarshaling{
		Name: "glastonbury",
		Inputs: []ethbinding.ABIArgumentMarshaling{
			{
				Name: "field",
				Type: "address",
			},
			{
				Name: "tents",
				Type: "uint256",
			},
			{
				Name: "mud",
				Type: "bool",
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
	assert.Equal("*:glastonbury(address,uint256,bool)", s1.info.Summary)
	// common.BytesToHash(crypto.Keccak256([]byte("glastonbury(address,uint256,bool)"))).Hex()
	assert.Equal("0x80f327694f71b67acac8d8c4b097d66a508a3cb6f8f27644c932bf508654a046", s.info.Filter.Topics[0][0].Hex())
}

func TestCreateWebhookSubWithAddr(t *testing.T) {
	assert := assert.New(t)

	rpc := eth.NewMockRPCClientForSync(nil, nil)
	m := &mockSubMgr{stream: newTestStream()}
	event := &ethbinding.ABIElementMarshaling{
		Name:      "devcon",
		Anonymous: true,
	}

	addr := ethbind.API.HexToAddress("0x0123456789abcDEF0123456789abCDef01234567")
	subInfo := testSubInfo(event)
	subInfo.Name = "mySubscription"
	s, err := newSubscription(m, rpc, &addr, subInfo)
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)
	// common.BytesToHash(crypto.Keccak256([]byte("devcon()"))).Hex()
	assert.Equal("0x81b7baac232325e8fb0e2446cc62852d9f68c86874699311b99ef89d8ed424dd", s.info.Filter.Topics[0][0].Hex())
	assert.Equal("0x0123456789abcDEF0123456789abCDef01234567:devcon()", s.info.Summary)
	assert.Equal("mySubscription", s.info.Name)
}

func TestCreateSubscriptionNoEvent(t *testing.T) {
	assert := assert.New(t)
	event := &ethbinding.ABIElementMarshaling{}
	m := &mockSubMgr{stream: newTestStream()}
	_, err := newSubscription(m, nil, nil, testSubInfo(event))
	assert.EqualError(err, "Solidity event name must be specified")
}

func TestCreateSubscriptionBadABI(t *testing.T) {
	assert := assert.New(t)
	event := &ethbinding.ABIElementMarshaling{
		Inputs: []ethbinding.ABIArgumentMarshaling{
			{Name: "badness", Type: "-1"},
		},
	}
	m := &mockSubMgr{stream: newTestStream()}
	_, err := newSubscription(m, nil, nil, testSubInfo(event))
	assert.EqualError(err, "invalid type '-1'")
}

func TestCreateSubscriptionMissingAction(t *testing.T) {
	assert := assert.New(t)
	event := &ethbinding.ABIElementMarshaling{Name: "party"}
	m := &mockSubMgr{err: fmt.Errorf("nope")}
	_, err := newSubscription(m, nil, nil, testSubInfo(event))
	assert.EqualError(err, "nope")
}

func TestRestoreSubscriptionMissingAction(t *testing.T) {
	assert := assert.New(t)
	m := &mockSubMgr{err: fmt.Errorf("nope")}
	_, err := restoreSubscription(m, nil, testSubInfo(&ethbinding.ABIElementMarshaling{}))
	assert.EqualError(err, "nope")
}

func TestRestoreSubscriptionBadType(t *testing.T) {
	assert := assert.New(t)
	event := &ethbinding.ABIElementMarshaling{
		Inputs: []ethbinding.ABIArgumentMarshaling{
			{Name: "badness", Type: "-1"},
		},
	}
	m := &mockSubMgr{stream: newTestStream()}
	_, err := restoreSubscription(m, nil, testSubInfo(event))
	assert.EqualError(err, "invalid type '-1'")
}

func TestProcessEventsStaleFilter(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		rpc: eth.NewMockRPCClientForSync(fmt.Errorf("filter not found"), nil),
	}
	err := s.processNewEvents(context.Background())
	assert.EqualError(err, "filter not found")
	assert.True(s.filterStale)
}

func TestProcessEventsCannotProcess(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		rpc: eth.NewMockRPCClientForSync(nil, func(method string, res interface{}, args ...interface{}) {
			les := res.(*[]*logEntry)
			*les = append(*les, &logEntry{
				Data: "0x no hex here sorry",
			})
		}),
		lp: newLogProcessor("", nil, newTestStream()),
	}
	err := s.processNewEvents(context.Background())
	// We swallow the error in this case - as we simply couldn't read the event
	assert.NoError(err)
}

func TestInitialFilterFail(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		info: &SubscriptionInfo{},
		rpc:  eth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil),
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
		rpc: eth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil),
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
		rpc:  eth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil),
	}
	err := s.restartFilter(context.Background(), big.NewInt(0))
	assert.EqualError(err, "eth_blockNumber returned: pop")
}

func TestCreateFilterFail(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		info: &SubscriptionInfo{},
		rpc:  eth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil),
	}
	err := s.createFilter(context.Background(), big.NewInt(0))
	assert.EqualError(err, "eth_newFilter returned: pop")
}

func TestProcessCatchupBlocksFail(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		info:         &SubscriptionInfo{},
		rpc:          eth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil),
		catchupBlock: big.NewInt(12345),
	}
	err := s.processCatchupBlocks(context.Background())
	assert.EqualError(err, "eth_getLogs returned: pop")
}

func TestEventTimestampFail(t *testing.T) {
	assert := assert.New(t)
	stream := newTestStream()
	lp := &logProcessor{stream: stream}

	s := &subscription{
		lp:   lp,
		info: &SubscriptionInfo{},
		rpc:  eth.NewMockRPCClientForSync(fmt.Errorf("pop"), nil),
	}
	l := &logEntry{Timestamp: 100} // set it to a fake value, should get overwritten
	s.getEventTimestamp(context.Background(), l)
	assert.Equal(l.Timestamp, uint64(0))
}

func TestUnsubscribe(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		rpc: eth.NewMockRPCClientForSync(nil, func(method string, res interface{}, args ...interface{}) {
			*(res.(*bool)) = true
		}),
	}
	err := s.unsubscribe(context.Background(), true)
	assert.NoError(err)
	assert.True(s.filterStale)
}

func TestLoadCheckpointBadJSON(t *testing.T) {
	assert := assert.New(t)
	sm := newTestSubscriptionManager()
	mockKV := kvstore.NewMockKV(nil)
	sm.db = mockKV
	mockKV.KVS[checkpointIDPrefix+"id1"] = []byte(":bad json")
	_, err := sm.loadCheckpoint("id1")
	assert.Error(err)
}
