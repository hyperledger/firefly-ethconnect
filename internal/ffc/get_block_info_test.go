// Copyright © 2022 Kaleido, Inc.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ffc

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/hyperledger/firefly-transaction-manager/pkg/ffcapi"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const sampleGetBlockInfoByNumber = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "get_block_info_by_number"
	},
	"blockNumber": "12345"
}`

const sampleGetBlockInfoByHash = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "get_block_info_by_hash"
	},
	"blockHash": "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6"
}`

const sampleBlockJSONRPC = `{
	"difficulty": "0x2",
	"extraData": "0xd683010a11846765746886676f312e3138856c696e7578000000000000000000ebe2ceb710450c390fbbf76e379cca8b5dac0444c2d49f5039b0fb61b9d6d0912ed4afe89227b39b21c78398824e9feb4b6d6f9f17c2b4c3bfa0e5975f3e12df01",
	"gasLimit": "0x48112a",
	"gasUsed": "0x8414",
	"hash": "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6",
	"logsBloom": "0x00000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000100000000000000000002000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000800000000000000",
	"miner": "0x0000000000000000000000000000000000000000",
	"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
	"nonce": "0x0000000000000000",
	"number": "0x3039",
	"parentHash": "0x124ca6245d8ddd48203346c2f80b9bc07ce2fcdb8ccb3251b03d8748c1c73b92",
	"receiptsRoot": "0x9b2a34bd8b935ade9cbdc016872e59d3abafe3f73d8471523cbb05b24fe2a620",
	"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
	"size": "0x44c",
	"stateRoot": "0x07990588ecb235a7d5a483e94347356b10c2a68e876c023c9eb78ee5706d4315",
	"timestamp": "0x625829cc",
	"totalDifficulty": "0xb",
	"transactions": [
	"0x7d48ae971faf089878b57e3c28e3035540d34f38af395958d2c73c36c57c83a2"
	],
	"transactionsRoot": "0x8ae1c0f1c985972257ed1719c6fb9524a3c5a43eaa5493fb83c00ca070d7a460",
	"uncles": []
}`

func TestGetBlockInfoByNumberOK(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber",
		mock.MatchedBy(
			func(blockNumber ethbinding.HexUint64) bool {
				return blockNumber.String() == "0x3039"
			}),
		false).
		Return(nil).
		Run(func(args mock.Arguments) {
			err := json.Unmarshal([]byte(sampleBlockJSONRPC), args[1])
			assert.NoError(t, err)
		})

	iRes, reason, err := s.getBlockInfoByNumber(ctx, []byte(sampleGetBlockInfoByNumber))
	assert.NoError(t, err)
	assert.Empty(t, reason)

	res := iRes.(*ffcapi.GetBlockInfoByNumberResponse)
	assert.Equal(t, "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6", res.BlockHash)
	assert.Equal(t, "0x124ca6245d8ddd48203346c2f80b9bc07ce2fcdb8ccb3251b03d8748c1c73b92", res.ParentHash)
	assert.Equal(t, int64(12345), res.BlockNumber.Int64())

}

func TestGetBlockInfoByNumberNotFound(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.Anything, false).
		Return(nil).
		Run(func(args mock.Arguments) {
			err := json.Unmarshal([]byte("null"), args[1])
			assert.NoError(t, err)
		})

	iRes, reason, err := s.getBlockInfoByNumber(ctx, []byte(sampleGetBlockInfoByNumber))
	assert.Regexp(t, "FFEC100218", err)
	assert.Equal(t, ffcapi.ErrorReasonNotFound, reason)
	assert.Nil(t, iRes)

}

func TestGetBlockInfoByNumberFail(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByNumber", mock.Anything, false).
		Return(fmt.Errorf("pop"))

	iRes, reason, err := s.getBlockInfoByNumber(ctx, []byte(sampleGetBlockInfoByNumber))
	assert.Regexp(t, "pop", err)
	assert.Empty(t, reason)
	assert.Nil(t, iRes)

}

func TestGetBlockInfoByNumberBadPayload(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()

	iRes, reason, err := s.getBlockInfoByNumber(ctx, []byte("!json"))
	assert.Regexp(t, "invalid", err)
	assert.Equal(t, ffcapi.ErrorReasonInvalidInputs, reason)
	assert.Nil(t, iRes)

}

func TestGetBlockInfoByHashOK(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6", false).
		Return(nil).
		Run(func(args mock.Arguments) {
			err := json.Unmarshal([]byte(sampleBlockJSONRPC), args[1])
			assert.NoError(t, err)
		})

	iRes, reason, err := s.getBlockInfoByHash(ctx, []byte(sampleGetBlockInfoByHash))
	assert.NoError(t, err)
	assert.Empty(t, reason)

	res := iRes.(*ffcapi.GetBlockInfoByHashResponse)
	assert.Equal(t, "0x6197ef1a58a2a592bb447efb651f0db7945de21aa8048801b250bd7b7431f9b6", res.BlockHash)
	assert.Equal(t, "0x124ca6245d8ddd48203346c2f80b9bc07ce2fcdb8ccb3251b03d8748c1c73b92", res.ParentHash)
	assert.Equal(t, int64(12345), res.BlockNumber.Int64())

}

func TestGetBlockInfoByHashNotFound(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.Anything, false).
		Return(nil).
		Run(func(args mock.Arguments) {
			err := json.Unmarshal([]byte("null"), args[1])
			assert.NoError(t, err)
		})

	iRes, reason, err := s.getBlockInfoByHash(ctx, []byte(sampleGetBlockInfoByHash))
	assert.Regexp(t, "FFEC100218", err)
	assert.Equal(t, ffcapi.ErrorReasonNotFound, reason)
	assert.Nil(t, iRes)

}

func TestGetBlockInfoByHashFail(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_getBlockByHash", mock.Anything, false).
		Return(fmt.Errorf("pop"))

	iRes, reason, err := s.getBlockInfoByHash(ctx, []byte(sampleGetBlockInfoByHash))
	assert.Regexp(t, "pop", err)
	assert.Empty(t, reason)
	assert.Nil(t, iRes)

}

func TestGetBlockInfoByHashBadPayload(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()

	iRes, reason, err := s.getBlockInfoByHash(ctx, []byte("!json"))
	assert.Regexp(t, "invalid", err)
	assert.Equal(t, ffcapi.ErrorReasonInvalidInputs, reason)
	assert.Nil(t, iRes)

}
