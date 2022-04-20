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
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/hyperledger/firefly-ethconnect/internal/eth"
	"github.com/hyperledger/firefly-transaction-manager/pkg/ffcapi"
	"github.com/hyperledger/firefly/pkg/fftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const samplePrepareTXWithGas = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "prepare_transaction"
	},
	"from": "0xb480F96c0a3d6E9e9a263e4665a39bFa6c4d01E8",
	"to": "0xe1a078b9e2b145d0a7387f09277c6ae1d9470771",
	"gas": 1000000,
	"nonce": "111",
	"value": "12345678901234567890123456789",
	"method": {
		"inputs": [],
		"name":"do",
		"outputs":[],
		"stateMutability":"nonpayable",
		"type":"function"
	},
	"params": []
}`

const samplePrepareTXEstimateGas = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "prepare_transaction"
	},
	"from": "0xb480F96c0a3d6E9e9a263e4665a39bFa6c4d01E8",
	"to": "0xe1a078b9e2b145d0a7387f09277c6ae1d9470771",
	"nonce": "222",
	"method": {
		"inputs": [],
		"name":"do",
		"outputs":[],
		"stateMutability":"nonpayable",
		"type":"function"
	},
	"method": {
		"inputs": [
			{
				"internalType":" uint256",
				"name": "x",
				"type": "uint256"
			}
		],
		"name":"set",
		"outputs":[],
		"stateMutability":"nonpayable",
		"type":"function"
	},
	"params": [ 4276993775 ]
}`

const samplePrepareTXBadMethod = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "prepare_transaction"
	},
	"from": "0xb480F96c0a3d6E9e9a263e4665a39bFa6c4d01E8",
	"to": "0xe1a078b9e2b145d0a7387f09277c6ae1d9470771",
	"gas": 1000000,
	"method": false,
	"params": []
}`

const samplePrepareTXBadParam = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "prepare_transaction"
	},
	"from": "0xb480F96c0a3d6E9e9a263e4665a39bFa6c4d01E8",
	"to": "0xe1a078b9e2b145d0a7387f09277c6ae1d9470771",
	"gas": 1000000,
	"nonce": "111",
	"method": {
		"inputs": [
			{
				"internalType":" uint256",
				"name": "x",
				"type": "uint256"
			}
		],
		"name":"set",
		"outputs":[],
		"stateMutability":"nonpayable",
		"type":"function"
	},
	"params": [ "wrong type" ]
}`

func TestPrepareTransactionOkNoEstimate(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()
	iRes, reason, err := s.prepareTransaction(ctx, []byte(samplePrepareTXWithGas))

	assert.NoError(t, err)
	assert.Empty(t, reason)

	res := iRes.(*ffcapi.PrepareTransactionResponse)
	assert.Equal(t, int64(1000000), res.Gas.Int64())

}

func TestPrepareTransactionWithEstimate(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_estimateGas",
		mock.MatchedBy(func(st *eth.SendTXArgs) bool {
			assert.Equal(t, "0x60fe47b100000000000000000000000000000000000000000000000000000000feedbeef", st.Data.String())
			return true
		})).
		Return(nil).
		Run(func(args mock.Arguments) {
			var gasEstimate hexutil.Uint64 = 12345
			**(args[1].(**hexutil.Uint64)) = gasEstimate
		})

	iRes, reason, err := s.prepareTransaction(ctx, []byte(samplePrepareTXEstimateGas))
	assert.NoError(t, err)
	assert.Empty(t, reason)

	res := iRes.(*ffcapi.PrepareTransactionResponse)
	assert.Equal(t, int64(14814) /* 1.2 uplift */, res.Gas.Int64())

}

func TestPrepareTransactionWithEstimateFail(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_estimateGas", mock.Anything).Return(fmt.Errorf("pop"))
	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_call", mock.Anything, "latest").Run(
		func(args mock.Arguments) {
			*(args[1].(*string)) = "0x08c379a0000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000114d75707065747279206465746563746564000000000000000000000000000000"
		},
	).Return(nil)

	iRes, reason, err := s.prepareTransaction(ctx, []byte(samplePrepareTXEstimateGas))
	assert.Regexp(t, "FFEC100149", err)
	assert.Equal(t, ffcapi.ErrorReasonTransactionReverted, reason)
	assert.Nil(t, iRes)

}

func TestPrepareTransactionWithBadMethod(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()

	iRes, reason, err := s.prepareTransaction(ctx, []byte(samplePrepareTXBadMethod))
	assert.Regexp(t, "FFEC100212", err)
	assert.Equal(t, ffcapi.ErrorReasonInvalidInputs, reason)
	assert.Nil(t, iRes)

}

func TestPrepareTransactionWithBadParam(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()

	iRes, reason, err := s.prepareTransaction(ctx, []byte(samplePrepareTXBadParam))
	assert.Regexp(t, "FFEC100160", err)
	assert.Equal(t, ffcapi.ErrorReasonInvalidInputs, reason)
	assert.Nil(t, iRes)

}

func TestPrepareTransactionWithBadPayload(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()

	iRes, reason, err := s.prepareTransaction(ctx, []byte("!json"))
	assert.Regexp(t, "invalid", err)
	assert.Equal(t, ffcapi.ErrorReasonInvalidInputs, reason)
	assert.Nil(t, iRes)

}

func TestMapFFCAPIToEthBadParams(t *testing.T) {

	s, _ := newTestFFCAPIServer()

	_, err := s.mapFFCAPIToEth(&ffcapi.PrepareTransactionRequest{
		TransactionPrepareInputs: ffcapi.TransactionPrepareInputs{
			Method: "{}",
			Params: []fftypes.JSONAny{"!wrong"},
		},
	})
	assert.Regexp(t, "FFEC100213", err)

}
