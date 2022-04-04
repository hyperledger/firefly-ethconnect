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

func TestBlockConfirmationManagerE2E(t *testing.T) {
	bcm, rpc := newTestBlockConfirmationManager(t, true)

	testStream := &eventStream{
		eventStream: make(chan *eventData, 1),
	}
	eventToConfirm := &eventData{
		TransactionHash: "0x531e219d98d81dc9f9a14811ac537479f5d77a74bdba47629bfbebe2d7663ce7",
		BlockNumber:     "1001",
		BlockHash:       "0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542",
		LogIndex:        "10",
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
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		blockHash := ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df") // block 1003
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{
			&blockHash,
		}
	}).Return(nil).Once()

	// The next filter gives us 1003 - which is two blocks ahead of our notified log
	block1003 := &blockInfo{
		Number:     1003,
		Hash:       ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
		ParentHash: ethbind.API.HexToHash("0x46210d224888265c269359529618bf2f6adb2697ff52c63c10f16a2391bdd295"),
	}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.MatchedBy(func(b *ethbinding.Hash) bool {
		return b.String() == "0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df" // block 1003
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1003
	}).Return(nil).Once()

	// Then we should walk the chain by number to fill in 1002/1003, because our HWM is 1003
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1002
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = &blockInfo{
			Number:     1002,
			Hash:       ethbind.API.HexToHash("0x46210d224888265c269359529618bf2f6adb2697ff52c63c10f16a2391bdd295"),
			ParentHash: ethbind.API.HexToHash("0x0e32d749a86cfaf551d528b5b121cea456f980a39e5b8136eb8e85dbc744a542"),
		}
	}).Return(nil).Once()

	// Then we should walk the chain by number to fill in 1002, because our HWM is 1003
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.MatchedBy(func(i hexutil.Uint64) bool {
		return uint64(i) == 1003
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = block1003
	}).Return(nil).Once()

	// Then we get notified of 1004 to complete the last confirmation
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.MatchedBy(func(i hexutil.Big) bool {
		return i.ToInt().Int64() == int64(1977)
	})).Run(func(args mock.Arguments) {
		blockHash := ethbinding.EthAPIShim.HexToHash("0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8")
		*(args[1].(*[]*ethbinding.Hash)) = []*ethbinding.Hash{
			&blockHash,
		}
	}).Return(nil).Once()

	// Which then gets downloaded, and should complete the confirmation
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.MatchedBy(func(b *ethbinding.Hash) bool {
		return b.String() == "0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8" // block 1004
	}), false).Run(func(args mock.Arguments) {
		*(args[1].(**blockInfo)) = &blockInfo{
			Number:     1004,
			Hash:       ethbind.API.HexToHash("0xed21f4f73d150f16f922ae82b7485cd936ae1eca4c027516311b928360a347e8"),
			ParentHash: ethbind.API.HexToHash("0x64fd8179b80dd255d52ce60d7f265c0506be810e2f3df52463fadeb44bb4d2df"),
		}
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
	}).Return(nil)

	bcm.start()

	dispatched := <-testStream.eventStream
	assert.Equal(t, eventToConfirm, dispatched)

	bcm.stop()
	<-bcm.done
}
