// Copyright 2022 Kaleido

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
	"sort"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/hyperledger/firefly-ethconnect/internal/ethbind"
	"github.com/hyperledger/firefly-ethconnect/mocks/ethmocks"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func newTestBlockConfirmationManager(t *testing.T, enabled bool) (*blockConfirmationManager, *ethmocks.RPCClient) {
	return newTestBlockConfirmationManagerConf(t, &bcmConfInternal{
		requiredConfirmations: 3,
		blockCacheSize:        defaultBlockCacheSize,
		pollingInterval:       1 * time.Millisecond,
		eventQueueLength:      1,
		includeInPayload:      true,
	})
}

func newTestBlockConfirmationManagerConf(t *testing.T, conf *bcmConfInternal) (*blockConfirmationManager, *ethmocks.RPCClient) {
	logrus.SetLevel(logrus.DebugLevel)
	rpc := &ethmocks.RPCClient{}
	bcm, err := newBlockConfirmationManager(context.Background(), rpc, conf)
	assert.NoError(t, err)
	return bcm, rpc
}

func TestBCMInitError(t *testing.T) {
	badNumber := -1
	_, err := newBlockConfirmationManager(context.Background(), nil, parseBCMConfig(&bcmConfExternal{
		BlockCacheSize: &badNumber,
	}))
	assert.Regexp(t, "FFEC100041", err)
}

func TestParseBCMConfig(t *testing.T) {
	requiredConfirmations := 32
	blockCacheSize := 100
	blockPollingIntervalSec := 15
	eventQueueLength := 1
	conf := parseBCMConfig(&bcmConfExternal{
		Enabled:                 true,
		RequiredConfirmations:   &requiredConfirmations,
		BlockCacheSize:          &blockCacheSize,
		BlockPollingIntervalSec: &blockPollingIntervalSec,
		EventQueueLength:        &eventQueueLength,
	})
	assert.Equal(t, &bcmConfInternal{
		requiredConfirmations: 32,
		blockCacheSize:        100,
		pollingInterval:       15 * time.Second,
		eventQueueLength:      1,
	}, conf)
}

func TestBlockConfirmationManagerE2ENewEvent(t *testing.T) {
	bcm, rpc := newTestBlockConfirmationManager(t, true)

	testStream := &eventStream{
		eventStream: make(chan *eventData, 1),
	}
	eventToConfirm := &eventData{
		TransactionHash:  "0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7",
		BlockHash:        "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542",
		blockNumber:      1001,
		transactionIndex: 5,
		logIndex:         10,
	}
	lastBlockDetected := false

	// Establish the block filter
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newBlockFilter").Run(func(args mock.Arguments) {
		args[1].(*hexutil.Big).ToInt().SetString("1977", 10)
	}).Return(nil).Once()

	// First poll for changes gives nothing, but we load up the event at this point for the next round
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{}

		bcm.notify(&bcmNotification{
			nType:       bcmNewLog,
			event:       eventToConfirm,
			eventStream: testStream,
		})
	}).Return(nil).Once()

	// Next time round gives a block that is in the confirmation chain, but one block ahead
	block1003 := &blockInfo{
		Number:     1003,
		Hash:       ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
		ParentHash: ethbind.API.HexToHash("0x46210d224888265c269359529618bf2f6adb2697ff52c63c10f16a2391bdd295"),
	}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{
			&block1003.Hash,
		}
	}).Return(nil).Once()

	// The next filter gives us 1003 - which is two blocks ahead of our notified log
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.MatchedBy(func(b *ethbinding.Hash) bool {
		return b.String() == block1003.Hash.String()
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1003
	}).Return(nil).Once()

	// Then we should walk the chain by number to fill in 1002/1003, because our HWM is 1003
	block1002 := &blockInfo{
		Number:     1002,
		Hash:       ethbind.API.HexToHash("0x46210d224888265c269359529618bf2f6adb2697ff52c63c10f16a2391bdd295"),
		ParentHash: ethbind.API.HexToHash("0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542"),
	}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1002
	}).Return(nil).Once()

	// Then we should walk the chain by number to fill in 1002, because our HWM is 1003.
	// Note this doesn't result in any RPC calls, as we just cached the block and it matches

	// Then we get notified of 1004 to complete the last confirmation
	block1004 := &blockInfo{
		Number:     1004,
		Hash:       ethbind.API.HexToHash("0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8"),
		ParentHash: ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
	}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{
			&block1004.Hash,
		}
	}).Return(nil).Once()

	// Which then gets downloaded, and should complete the confirmation
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.MatchedBy(func(b *ethbinding.Hash) bool {
		return b.String() == block1004.Hash.String()
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1004
		lastBlockDetected = true
	}).Return(nil).Once()

	// Subsequent calls get nothing, and blocks until close anyway
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{}
		if lastBlockDetected {
			<-bcm.ctx.Done()
		}
	}).Return(nil).Maybe()

	bcm.start()

	dispatched := <-testStream.eventStream
	assert.Equal(t, eventToConfirm, dispatched)

	bcm.stop()
	<-bcm.done

	rpc.AssertExpectations(t)
}

func TestBlockConfirmationManagerE2EFork(t *testing.T) {
	bcm, rpc := newTestBlockConfirmationManager(t, true)

	testStream := &eventStream{
		eventStream: make(chan *eventData, 1),
	}
	eventToConfirm := &eventData{
		TransactionHash:  "0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7",
		BlockHash:        "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542",
		blockNumber:      1001,
		transactionIndex: 5,
		logIndex:         10,
	}
	lastBlockDetected := false

	// Establish the block filter
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newBlockFilter").Run(func(args mock.Arguments) {
		args[1].(*hexutil.Big).ToInt().SetString("1977", 10)
	}).Return(nil).Once()

	// The next filter gives us 1002, and a first 1003 block - which will later be removed
	block1002 := &blockInfo{
		Number:     1002,
		Hash:       ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
		ParentHash: ethbind.API.HexToHash("0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542"),
	}
	block1003a := &blockInfo{
		Number:     1003,
		Hash:       ethbind.API.HexToHash("0x46210d224888265c269359529618bf2f6adb2697ff52c63c10f16a2391bdd295"),
		ParentHash: ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
	}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{
			&block1002.Hash,
			&block1003a.Hash,
		}
	}).Return(nil).Once()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.MatchedBy(func(b *ethbinding.Hash) bool {
		return b.String() == block1002.Hash.String()
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1002
	}).Return(nil).Once()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.MatchedBy(func(b *ethbinding.Hash) bool {
		return b.String() == block1003a.Hash.String()
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1003a
	}).Return(nil).Once()

	// Then we get the final fork up to our confirmation
	block1003b := &blockInfo{
		Number:     1003,
		Hash:       ethbind.API.HexToHash("0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8"),
		ParentHash: ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
	}
	block1004 := &blockInfo{
		Number:     1004,
		Hash:       ethbind.API.HexToHash("0x110282339db2dfe4bfd13d78375f7883048cac6bc12f8393bd080a4e263d5d21"),
		ParentHash: ethbind.API.HexToHash("0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8"),
	}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{
			&block1003b.Hash,
			&block1004.Hash,
		}
	}).Return(nil).Once()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.MatchedBy(func(b *ethbinding.Hash) bool {
		return b.String() == block1003b.Hash.String()
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1003b
	}).Return(nil).Once()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.MatchedBy(func(b *ethbinding.Hash) bool {
		return b.String() == block1004.Hash.String()
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1004
	}).Return(nil).Once()

	// Subsequent calls get nothing, and blocks until close anyway
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{}
		if lastBlockDetected {
			<-bcm.ctx.Done()
		}
	}).Return(nil)

	bcm.start()

	bcm.notify(&bcmNotification{
		nType:       bcmNewLog,
		event:       eventToConfirm,
		eventStream: testStream,
	})

	dispatched := <-testStream.eventStream
	assert.Equal(t, eventToConfirm, dispatched)
	assert.Len(t, eventToConfirm.Confirmations, 3)
	assert.Equal(t, block1002.Hash.String(), eventToConfirm.Confirmations[0].Hash.String())
	assert.Equal(t, block1003b.Hash.String(), eventToConfirm.Confirmations[1].Hash.String())
	assert.Equal(t, block1004.Hash.String(), eventToConfirm.Confirmations[2].Hash.String())

	bcm.stop()
	<-bcm.done

	rpc.AssertExpectations(t)

}

func TestBlockConfirmationManagerE2EHistoricalEvent(t *testing.T) {
	bcm, rpc := newTestBlockConfirmationManager(t, true)

	testStream := &eventStream{
		eventStream: make(chan *eventData, 1),
	}
	eventToConfirm := &eventData{
		TransactionHash:  "0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7",
		BlockHash:        "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542",
		blockNumber:      1001,
		transactionIndex: 5,
		logIndex:         10,
	}
	bcm.notify(&bcmNotification{
		nType:       bcmNewLog,
		event:       eventToConfirm,
		eventStream: testStream,
	})

	// Establish the block filter
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newBlockFilter").Run(func(args mock.Arguments) {
		args[1].(*hexutil.Big).ToInt().SetString("1977", 10)
	}).Return(nil).Once()

	// We don't notify of any new blocks
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{}
	}).Return(nil)

	// Then we should walk the chain by number to fill in 1002/1003, because our HWM is 1003
	block1002 := &blockInfo{
		Number:     1002,
		Hash:       ethbind.API.HexToHash("0x46210d224888265c269359529618bf2f6adb2697ff52c63c10f16a2391bdd295"),
		ParentHash: ethbind.API.HexToHash("0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542"),
	}
	block1003 := &blockInfo{
		Number:     1003,
		Hash:       ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
		ParentHash: ethbind.API.HexToHash("0x46210d224888265c269359529618bf2f6adb2697ff52c63c10f16a2391bdd295"),
	}
	block1004 := &blockInfo{
		Number:     1004,
		Hash:       ethbind.API.HexToHash("0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8"),
		ParentHash: ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
	}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1002
	}).Return(nil).Once()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1003
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1003
	}).Return(nil).Once()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1004
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1004
	}).Return(nil).Once()

	bcm.start()

	dispatched := <-testStream.eventStream
	assert.Equal(t, eventToConfirm, dispatched)

	bcm.stop()
	<-bcm.done

	rpc.AssertExpectations(t)
}

func TestSortPendingEvents(t *testing.T) {
	events := pendingEvents{
		{event: &eventData{blockNumber: 1000, transactionIndex: 10, logIndex: 2}},
		{event: &eventData{blockNumber: 1003, transactionIndex: 0, logIndex: 10}},
		{event: &eventData{blockNumber: 1000, transactionIndex: 5, logIndex: 5}},
		{event: &eventData{blockNumber: 1000, transactionIndex: 10, logIndex: 0}},
		{event: &eventData{blockNumber: 1002, transactionIndex: 0, logIndex: 0}},
	}
	sort.Sort(events)
	assert.Equal(t, pendingEvents{
		{event: &eventData{blockNumber: 1000, transactionIndex: 5, logIndex: 5}},
		{event: &eventData{blockNumber: 1000, transactionIndex: 10, logIndex: 0}},
		{event: &eventData{blockNumber: 1000, transactionIndex: 10, logIndex: 2}},
		{event: &eventData{blockNumber: 1002, transactionIndex: 0, logIndex: 0}},
		{event: &eventData{blockNumber: 1003, transactionIndex: 0, logIndex: 10}},
	}, events)
}

func TestMarshalJSONBlockInfo(t *testing.T) {
	bi := &blockInfo{
		Number:     1003,
		Hash:       ethbind.API.HexToHash("0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8"),
		ParentHash: ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
		Timestamp:  1649077248,
	}
	d, err := json.Marshal(&bi)
	assert.NoError(t, err)
	assert.JSONEq(t, `
	{
		"hash":"0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8",
		"number":"1003",
		"parentHash":"0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df",
		"timestamp":"1649077248"
	}`, string(d))
}

func TestCreateBlockFilterFail(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)
	bcm.done = make(chan struct{})

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newBlockFilter").Run(func(args mock.Arguments) {
		bcm.cancelFunc()
	}).Return(fmt.Errorf("pop")).Once()

	bcm.confirmationsListener()

	rpc.AssertExpectations(t)
}

func TestConfirmationsListenerFailWalkingChain(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)
	bcm.done = make(chan struct{})

	testStream := &eventStream{
		eventStream: make(chan *eventData, 1),
	}
	eventToConfirm := &eventData{
		TransactionHash:  "0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7",
		BlockHash:        "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542",
		blockNumber:      1001,
		transactionIndex: 5,
		logIndex:         10,
	}
	bcm.addEvent(eventToConfirm, testStream)

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newBlockFilter").Run(func(args mock.Arguments) {
		args[1].(*hexutil.Big).ToInt().SetString("1977", 10)
		bcm.cancelFunc()
	}).Return(nil).Once()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Return(fmt.Errorf("pop")).Once()

	bcm.confirmationsListener()

	rpc.AssertExpectations(t)
}

func TestConfirmationsListenerFailPollingBlocks(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)
	bcm.done = make(chan struct{})

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newBlockFilter").Run(func(args mock.Arguments) {
		args[1].(*hexutil.Big).ToInt().SetString("1977", 10)
		bcm.cancelFunc()
	}).Return(nil).Once()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == 1977
	})).Return(fmt.Errorf("pop")).Once()

	bcm.confirmationsListener()

	rpc.AssertExpectations(t)
}

func TestConfirmationsListenerLostFilterReestablish(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)
	bcm.done = make(chan struct{})

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newBlockFilter").Run(func(args mock.Arguments) {
		args[1].(*hexutil.Big).ToInt().SetString("1977", 10)
	}).Return(nil).Twice()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == 1977
	})).Return(fmt.Errorf("filter not found")).Once()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		bcm.cancelFunc()
		return i.ToInt().Int64() == 1977
	})).Return(nil).Once()

	bcm.confirmationsListener()

	rpc.AssertExpectations(t)
}

func TestConfirmationsListenerFailWalkingChainForNewEvent(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)
	bcm.done = make(chan struct{})

	testStream := &eventStream{
		eventStream: make(chan *eventData, 1),
	}
	eventToConfirm := &eventData{
		TransactionHash:  "0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7",
		BlockHash:        "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542",
		blockNumber:      1001,
		transactionIndex: 5,
		logIndex:         10,
	}
	bcm.notify(&bcmNotification{
		nType:       bcmNewLog,
		event:       eventToConfirm,
		eventStream: testStream,
	})

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newBlockFilter").Run(func(args mock.Arguments) {
		args[1].(*hexutil.Big).ToInt().SetString("1977", 10)
		bcm.cancelFunc()
	}).Return(nil).Once()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{}
	}).Return(nil)
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Return(fmt.Errorf("pop")).Once()

	bcm.confirmationsListener()

	rpc.AssertExpectations(t)
}

func TestConfirmationsListenerStopStream(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)
	bcm.done = make(chan struct{})

	testStream := &eventStream{
		eventStream: make(chan *eventData, 1),
	}
	eventToConfirm := &eventData{
		TransactionHash:  "0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7",
		BlockHash:        "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542",
		blockNumber:      1001,
		transactionIndex: 5,
		logIndex:         10,
	}
	completed := make(chan struct{})
	bcm.addEvent(eventToConfirm, testStream)
	bcm.notify(&bcmNotification{
		nType:       bcmStopStream,
		eventStream: testStream,
		complete:    completed,
	})

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newBlockFilter").Run(func(args mock.Arguments) {
		args[1].(*hexutil.Big).ToInt().SetString("1977", 10)
	}).Return(nil)
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{}
	}).Return(nil)
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = nil
	}).Return(nil).Maybe()

	bcm.start()

	<-completed
	assert.Empty(t, bcm.pending)

	bcm.stop()
	rpc.AssertExpectations(t)
}

func TestConfirmationsRemoveEvent(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)
	bcm.done = make(chan struct{})

	testStream := &eventStream{
		eventStream: make(chan *eventData, 1),
	}
	event := &eventData{
		TransactionHash:  "0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7",
		BlockHash:        "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542",
		blockNumber:      1001,
		transactionIndex: 5,
		logIndex:         10,
	}
	bcm.addEvent(event, testStream)
	bcm.notify(&bcmNotification{
		nType: bcmRemovedLog,
		event: event,
	})

	changeCount := 0
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newBlockFilter").Run(func(args mock.Arguments) {
		args[1].(*hexutil.Big).ToInt().SetString("1977", 10)
	}).Return(nil).Maybe()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{}
		changeCount++
		if changeCount > 1 {
			bcm.cancelFunc()
		}
	}).Return(nil)
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = nil
	}).Return(nil).Once()

	bcm.start()
	<-bcm.done

	assert.Empty(t, bcm.pending)
	rpc.AssertExpectations(t)
}

func TestWalkChainForEventBlockNotAvailable(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)

	testStream := &eventStream{}
	pendingEvent := bcm.addEvent(&eventData{
		TransactionHash:  "0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7",
		BlockHash:        "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542",
		blockNumber:      1001,
		transactionIndex: 5,
		logIndex:         10,
	}, testStream)

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = nil
	}).Return(nil).Once()

	err := bcm.walkChainForEvent(pendingEvent)
	assert.NoError(t, err)

	rpc.AssertExpectations(t)
}

func TestWalkChainForEventBlockNotInConfirmationChain(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)

	testStream := &eventStream{}
	pendingEvent := bcm.addEvent(&eventData{
		TransactionHash:  "0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7",
		BlockHash:        "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542",
		blockNumber:      1001,
		transactionIndex: 5,
		logIndex:         10,
	}, testStream)

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = &blockInfo{
			Number:     1002,
			Hash:       ethbind.API.HexToHash("0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8"),
			ParentHash: ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
		}
	}).Return(nil).Once()

	err := bcm.walkChainForEvent(pendingEvent)
	assert.NoError(t, err)

	rpc.AssertExpectations(t)
}

func TestWalkChainForEventBlockLookupFail(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)

	testStream := &eventStream{}
	pendingEvent := bcm.addEvent(&eventData{
		TransactionHash:  "0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7",
		BlockHash:        "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542",
		blockNumber:      1001,
		transactionIndex: 5,
		logIndex:         10,
	}, testStream)

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Return(fmt.Errorf("pop")).Once()

	err := bcm.walkChainForEvent(pendingEvent)
	assert.Regexp(t, "pop", err)

	rpc.AssertExpectations(t)
}

func TestProcessBlockHashesLookupFail(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)

	blockHash := ethbind.API.HexToHash("0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8")
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.MatchedBy(func(b *ethbinding.Hash) bool {
		return b.String() == blockHash.String()
	}), false).Return(fmt.Errorf("pop")).Once()

	bcm.processBlockHashes([]*ethbinding.Hash{
		&blockHash,
	})

	rpc.AssertExpectations(t)
}

func TestProcessNotificationsSwallowsUnknownType(t *testing.T) {

	bcm, _ := newTestBlockConfirmationManager(t, false)
	bcm.processNotifications([]*bcmNotification{
		{nType: bcmEventType(999)},
	})
}

func TestGetBlockByNumberForceLookupMismatchedBlockType(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)

	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = &blockInfo{
			Number:     1002,
			Hash:       ethbind.API.HexToHash("0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8"),
			ParentHash: ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
		}
	}).Return(nil).Once()
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = &blockInfo{
			Number:     1002,
			Hash:       ethbind.API.HexToHash("0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7"),
			ParentHash: ethbind.API.HexToHash("0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542"),
		}
	}).Return(nil).Once()

	// Make the first call that caches
	blockInfo, err := bcm.getBlockByNumber(1002, "0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df")
	assert.NoError(t, err)
	assert.Equal(t, "0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df", blockInfo.ParentHash.String())

	// Make second call that is cached as parent matches
	blockInfo, err = bcm.getBlockByNumber(1002, "0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df")
	assert.NoError(t, err)
	assert.Equal(t, "0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df", blockInfo.ParentHash.String())

	// Make third call that does not as parent mismatched
	blockInfo, err = bcm.getBlockByNumber(1002, "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542")
	assert.NoError(t, err)
	assert.Equal(t, "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542", blockInfo.ParentHash.String())

}

func TestGetBlockByHashCached(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)

	block1003 := &blockInfo{
		Number:     1003,
		Hash:       ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
		ParentHash: ethbind.API.HexToHash("0x46210d224888265c269359529618bf2f6adb2697ff52c63c10f16a2391bdd295"),
	}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.MatchedBy(func(b *ethbinding.Hash) bool {
		return b.String() == block1003.Hash.String()
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1003
	}).Return(nil).Once()

	blockInfo, err := bcm.getBlockByHash(&block1003.Hash)
	assert.NoError(t, err)
	assert.Equal(t, block1003, blockInfo)

	// Get again cached
	blockInfo, err = bcm.getBlockByHash(&block1003.Hash)
	assert.NoError(t, err)
	assert.Equal(t, block1003, blockInfo)

}

func TestGetBlockNotFound(t *testing.T) {

	bcm, rpc := newTestBlockConfirmationManager(t, false)

	blockHash := ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df")
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.MatchedBy(func(b *ethbinding.Hash) bool {
		return b.String() == blockHash.String()
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = nil
	}).Return(nil).Once()

	blockInfo, err := bcm.getBlockByHash(&blockHash)
	assert.NoError(t, err)
	assert.Nil(t, blockInfo)

}
