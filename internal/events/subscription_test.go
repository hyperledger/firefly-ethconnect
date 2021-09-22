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
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	"github.com/hyperledger-labs/firefly-ethconnect/internal/contractregistry"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/eth"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/ethbind"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/kvstore"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/messages"
	"github.com/hyperledger-labs/firefly-ethconnect/mocks/contractregistrymocks"
	"github.com/hyperledger-labs/firefly-ethconnect/mocks/ethmocks"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

	rpc := &ethmocks.RPCClient{}
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
	s, err := newSubscription(m, rpc, nil, nil, i)
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)

	s1, err := restoreSubscription(m, rpc, nil, i)
	assert.NoError(err)

	assert.Equal(s.info.ID, s1.info.ID)
	assert.Equal("*:glastonbury(address,uint256,bool)", s1.info.Name)
	assert.Equal("*:glastonbury(address,uint256,bool)", s1.info.Summary)
	// common.BytesToHash(crypto.Keccak256([]byte("glastonbury(address,uint256,bool)"))).Hex()
	assert.Equal("0x80f327694f71b67acac8d8c4b097d66a508a3cb6f8f27644c932bf508654a046", s.info.Filter.Topics[0][0].Hex())
}

func TestCreateWebhookSubWithAddr(t *testing.T) {
	assert := assert.New(t)

	rpc := &ethmocks.RPCClient{}
	m := &mockSubMgr{stream: newTestStream()}
	event := &ethbinding.ABIElementMarshaling{
		Name:      "devcon",
		Anonymous: true,
	}

	addr := ethbind.API.HexToAddress("0x0123456789abcDEF0123456789abCDef01234567")
	subInfo := testSubInfo(event)
	subInfo.Name = "mySubscription"
	s, err := newSubscription(m, rpc, nil, &addr, subInfo)
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
	_, err := newSubscription(m, nil, nil, nil, testSubInfo(event))
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
	_, err := newSubscription(m, nil, nil, nil, testSubInfo(event))
	assert.EqualError(err, "invalid type '-1'")
}

func TestCreateSubscriptionMissingAction(t *testing.T) {
	assert := assert.New(t)
	event := &ethbinding.ABIElementMarshaling{Name: "party"}
	m := &mockSubMgr{err: fmt.Errorf("nope")}
	_, err := newSubscription(m, nil, nil, nil, testSubInfo(event))
	assert.EqualError(err, "nope")
}

func TestRestoreSubscriptionMissingAction(t *testing.T) {
	assert := assert.New(t)
	m := &mockSubMgr{err: fmt.Errorf("nope")}
	_, err := restoreSubscription(m, nil, nil, testSubInfo(&ethbinding.ABIElementMarshaling{}))
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
	_, err := restoreSubscription(m, nil, nil, testSubInfo(event))
	assert.EqualError(err, "invalid type '-1'")
}

func TestProcessEventsStaleFilter(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterLogs", mock.Anything).
		Return(fmt.Errorf("filter not found"))
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_uninstallFilter", mock.Anything).Return(nil)
	s := &subscription{rpc: rpc}
	err := s.processNewEvents(context.Background())
	assert.EqualError(err, "filter not found")
	assert.True(s.filterStale)
}

func TestProcessEventsCannotProcess(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterLogs", mock.Anything).
		Run(func(args mock.Arguments) {
			res := args[1]
			les := res.(*[]*logEntry)
			*les = append(*les, &logEntry{
				Data: "0x no hex here sorry",
			})
		}).
		Return(nil)
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_uninstallFilter", mock.Anything).Return(nil)
	s := &subscription{
		rpc: rpc,
		lp:  newLogProcessor("", &ethbinding.ABIEvent{}, newTestStream()),
	}
	err := s.processNewEvents(context.Background())
	// We swallow the error in this case - as we simply couldn't read the event
	assert.NoError(err)
}

func TestInitialFilterFail(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_blockNumber").
		Return(fmt.Errorf("pop"))
	s := &subscription{
		info: &SubscriptionInfo{},
		rpc:  rpc,
	}
	_, err := s.setInitialBlockHeight(context.Background())
	assert.EqualError(err, "eth_blockNumber returned: pop")
	rpc.AssertExpectations(t)
}

func TestInitialFilterBadInitialBlock(t *testing.T) {
	assert := assert.New(t)
	s := &subscription{
		info: &SubscriptionInfo{
			FromBlock: "!integer",
		},
		rpc: &ethmocks.RPCClient{},
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
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_blockNumber").
		Return(fmt.Errorf("pop"))
	s := &subscription{
		info: &SubscriptionInfo{},
		rpc:  rpc,
	}
	err := s.restartFilter(context.Background(), big.NewInt(0))
	assert.EqualError(err, "eth_blockNumber returned: pop")
	rpc.AssertExpectations(t)
}

func TestCreateFilterFail(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newFilter", mock.Anything).
		Return(fmt.Errorf("pop"))
	s := &subscription{
		info: &SubscriptionInfo{},
		rpc:  rpc,
	}
	err := s.createFilter(context.Background(), big.NewInt(0))
	assert.EqualError(err, "eth_newFilter returned: pop")
	rpc.AssertExpectations(t)
}

func TestProcessCatchupBlocksFail(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getLogs", mock.Anything).
		Return(fmt.Errorf("pop"))
	s := &subscription{
		info:         &SubscriptionInfo{},
		rpc:          rpc,
		catchupBlock: big.NewInt(12345),
	}
	err := s.processCatchupBlocks(context.Background())
	assert.EqualError(err, "eth_getLogs returned: pop")
	rpc.AssertExpectations(t)
}

func TestEventTimestampFail(t *testing.T) {
	assert := assert.New(t)
	stream := newTestStream()
	lp := &logProcessor{stream: stream}
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.Anything, mock.Anything).
		Return(fmt.Errorf("pop"))
	s := &subscription{
		lp:   lp,
		info: &SubscriptionInfo{},
		rpc:  rpc,
	}
	l := &logEntry{Timestamp: 100} // set it to a fake value, should get overwritten
	s.getEventTimestamp(context.Background(), l)
	assert.Equal(l.Timestamp, uint64(0))
	rpc.AssertExpectations(t)
}

func TestUnsubscribe(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_uninstallFilter", mock.Anything).
		Return(fmt.Errorf("pop"))
	s := &subscription{
		rpc: rpc,
	}
	err := s.unsubscribe(context.Background(), true)
	assert.NoError(err)
	assert.True(s.filterStale)
	rpc.AssertExpectations(t)
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

func TestGetTransactionInputsNoABI(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	cr := &contractregistrymocks.ContractStore{}

	s := &subscription{
		info: &SubscriptionInfo{},
		rpc:  rpc,
		cr:   cr,
	}
	l := logEntry{}
	lCopy := l
	s.getTransactionInputs(context.Background(), &l)

	result, err := json.Marshal(l)
	assert.NoError(err)
	defaultLogEntry, err := json.Marshal(lCopy)
	assert.NoError(err)
	assert.Equal(string(defaultLogEntry), string(result))
}

func TestGetTransactionInputsLoadABIFail(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	cr := &contractregistrymocks.ContractStore{}
	cr.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.LocalABI,
		Name:    "abi1",
	}, false).Return(nil, "", fmt.Errorf("pop"))

	s := &subscription{
		info: &SubscriptionInfo{
			ABI: &contractregistry.ABILocation{
				ABIType: contractregistry.LocalABI,
				Name:    "abi1",
			},
		},
		rpc: rpc,
		cr:  cr,
	}
	l := logEntry{}
	lCopy := l
	s.getTransactionInputs(context.Background(), &l)

	result, err := json.Marshal(l)
	assert.NoError(err)
	defaultLogEntry, err := json.Marshal(lCopy)
	assert.NoError(err)
	assert.Equal(string(defaultLogEntry), string(result))
}

func TestGetTransactionInputsMissingABI(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	cr := &contractregistrymocks.ContractStore{}
	cr.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.LocalABI,
		Name:    "abi1",
	}, false).Return(nil, "", nil)

	s := &subscription{
		info: &SubscriptionInfo{
			ABI: &contractregistry.ABILocation{
				ABIType: contractregistry.LocalABI,
				Name:    "abi1",
			},
		},
		rpc: rpc,
		cr:  cr,
	}
	l := logEntry{}
	lCopy := l
	s.getTransactionInputs(context.Background(), &l)

	result, err := json.Marshal(l)
	assert.NoError(err)
	defaultLogEntry, err := json.Marshal(&lCopy)
	assert.NoError(err)
	assert.Equal(string(defaultLogEntry), string(result))
}

func TestGetTransactionInputsTxnInfoFail(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	cr := &contractregistrymocks.ContractStore{}

	deployMsg := messages.DeployContract{}
	cr.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.LocalABI,
		Name:    "abi1",
	}, false).Return(&deployMsg, "", nil)

	s := &subscription{
		info: &SubscriptionInfo{
			ABI: &contractregistry.ABILocation{
				ABIType: contractregistry.LocalABI,
				Name:    "abi1",
			},
		},
		rpc: rpc,
		cr:  cr,
	}
	l := logEntry{
		TransactionHash: [32]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
	}
	lCopy := l

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getTransactionByHash", "0x0000000000000000000000000000000000000000000000000000000000000001").
		Return(fmt.Errorf("pop"))

	s.getTransactionInputs(context.Background(), &l)

	result, err := json.Marshal(l)
	assert.NoError(err)
	defaultLogEntry, err := json.Marshal(lCopy)
	assert.NoError(err)
	assert.Equal(string(defaultLogEntry), string(result))
}

func TestGetTransactionInputsBadMethod(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	cr := &contractregistrymocks.ContractStore{}

	deployMsg := messages.DeployContract{}
	cr.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.LocalABI,
		Name:    "abi1",
	}, false).Return(&deployMsg, "", nil)

	s := &subscription{
		info: &SubscriptionInfo{
			ABI: &contractregistry.ABILocation{
				ABIType: contractregistry.LocalABI,
				Name:    "abi1",
			},
		},
		rpc: rpc,
		cr:  cr,
	}
	l := logEntry{
		TransactionHash: [32]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
	}
	lCopy := l

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getTransactionByHash", "0x0000000000000000000000000000000000000000000000000000000000000001").
		Run(func(args mock.Arguments) {
			res := args[1]
			*(res.(*eth.TxnInfo)) = eth.TxnInfo{
				Input: &ethbinding.HexBytes{},
			}
		}).
		Return(nil)

	s.getTransactionInputs(context.Background(), &l)

	result, err := json.Marshal(l)
	assert.NoError(err)
	defaultLogEntry, err := json.Marshal(lCopy)
	assert.NoError(err)
	assert.Equal(string(defaultLogEntry), string(result))
}

func TestGetTransactionInputsSuccess(t *testing.T) {
	assert := assert.New(t)
	rpc := &ethmocks.RPCClient{}
	cr := &contractregistrymocks.ContractStore{}

	deployMsg := &messages.DeployContract{
		ABI: ethbinding.ABIMarshaling{
			{
				Type: "function",
				Name: "method1",
				Inputs: []ethbinding.ABIArgumentMarshaling{
					{
						Name: "arg1",
						Type: "int32",
					},
				},
			},
		},
	}
	methodInput := &ethbinding.HexBytes{
		0xf4, 0xe1, 0x3d, 0xc5, // ID of method1
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, // "1" as int32
	}
	expectedArgs := map[string]interface{}{"arg1": "1"}

	cr.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.LocalABI,
		Name:    "abi1",
	}, false).Return(deployMsg, "", nil)

	s := &subscription{
		info: &SubscriptionInfo{
			ABI: &contractregistry.ABILocation{
				ABIType: contractregistry.LocalABI,
				Name:    "abi1",
			},
		},
		rpc: rpc,
		cr:  cr,
	}
	l := logEntry{
		TransactionHash: [32]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
	}
	lCopy := l

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getTransactionByHash", "0x0000000000000000000000000000000000000000000000000000000000000001").
		Run(func(args mock.Arguments) {
			res := args[1]
			*(res.(*eth.TxnInfo)) = eth.TxnInfo{
				Input: methodInput,
			}
		}).
		Return(nil)

	s.getTransactionInputs(context.Background(), &l)

	result, err := json.Marshal(l)
	assert.NoError(err)
	lCopy.InputMethod = "method1"
	lCopy.InputArgs = expectedArgs
	defaultLogEntry, err := json.Marshal(lCopy)
	assert.NoError(err)
	assert.Equal(string(defaultLogEntry), string(result))
}
