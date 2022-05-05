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

package ffcapiconnector

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperledger/firefly-common/pkg/ffcapi"
	"github.com/hyperledger/firefly-ethconnect/internal/eth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const sampleSendTX = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "send_transaction"
	},
	"from": "0xb480F96c0a3d6E9e9a263e4665a39bFa6c4d01E8",
	"to": "0xe1a078b9e2b145d0a7387f09277c6ae1d9470771",
	"gas": 1000000,
	"nonce": "111",
	"value": "12345678901234567890123456789",
	"transactionData": "0x60fe47b100000000000000000000000000000000000000000000000000000000feedbeef"
}`

const sampleSendTXBadInputs = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "send_transaction"
	}
}`

const sampleSendTXBadData = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "send_transaction"
	},
	"transactionData": "not hex"
}`

const sampleSendTXBadGasPrice = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "send_transaction"
	},
	"gasPrice": "not a number"
}`

func TestSendTransactionOK(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_sendTransaction",
		mock.MatchedBy(func(st *eth.SendTXArgs) bool {
			assert.Equal(t, "0x60fe47b100000000000000000000000000000000000000000000000000000000feedbeef", st.Data.String())
			return true
		})).
		Run(func(args mock.Arguments) {
			*(args[1].(*string)) = "0x12345"
		}).
		Return(nil)

	iRes, reason, err := s.sendTransaction(ctx, []byte(sampleSendTX))
	assert.NoError(t, err)
	assert.Empty(t, reason)

	res := iRes.(*ffcapi.SendTransactionResponse)
	assert.Equal(t, "0x12345", res.TransactionHash)

	mRPC.AssertExpectations(t)

}

func TestSendTransactionFail(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_sendTransaction",
		mock.MatchedBy(func(st *eth.SendTXArgs) bool {
			assert.Equal(t, "0x60fe47b100000000000000000000000000000000000000000000000000000000feedbeef", st.Data.String())
			return true
		})).
		Return(fmt.Errorf("pop"))

	iRes, reason, err := s.sendTransaction(ctx, []byte(sampleSendTX))
	assert.Regexp(t, "pop", err)
	assert.Empty(t, reason)
	assert.Nil(t, iRes)

	mRPC.AssertExpectations(t)

}

func TestSendErrorMapping(t *testing.T) {

	assert.Equal(t, ffcapi.ErrorReasonNonceTooLow, mapError(sendRPCMethods, fmt.Errorf("nonce too low")))
	assert.Equal(t, ffcapi.ErrorReasonInsufficientFunds, mapError(sendRPCMethods, fmt.Errorf("insufficient funds")))
	assert.Equal(t, ffcapi.ErrorReasonTransactionUnderpriced, mapError(sendRPCMethods, fmt.Errorf("transaction underpriced")))
	assert.Equal(t, ffcapi.ErrorKnownTransaction, mapError(sendRPCMethods, fmt.Errorf("known transaction")))

}

func TestSendTransactionBadInputs(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()

	iRes, reason, err := s.sendTransaction(ctx, []byte(sampleSendTXBadInputs))
	assert.Regexp(t, "FFEC100156", err)
	assert.Equal(t, ffcapi.ErrorReasonInvalidInputs, reason)
	assert.Nil(t, iRes)

}

func TestSendTransactionBadData(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()

	iRes, reason, err := s.sendTransaction(ctx, []byte(sampleSendTXBadData))
	assert.Regexp(t, "FFEC100215", err)
	assert.Equal(t, ffcapi.ErrorReasonInvalidInputs, reason)
	assert.Nil(t, iRes)

}

func TestSendTransactionBadGasPrice(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()

	iRes, reason, err := s.sendTransaction(ctx, []byte(sampleSendTXBadGasPrice))
	assert.Regexp(t, "FFEC100214", err)
	assert.Equal(t, ffcapi.ErrorReasonInvalidInputs, reason)
	assert.Nil(t, iRes)

}

func TestSendTransactionBadPayload(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()

	iRes, reason, err := s.sendTransaction(ctx, []byte("!not json!"))
	assert.Regexp(t, "invalid", err)
	assert.Equal(t, ffcapi.ErrorReasonInvalidInputs, reason)
	assert.Nil(t, iRes)

}
