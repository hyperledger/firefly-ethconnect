// Copyright 2018, 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldeth

import (
	"context"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestGetTXReceiptMined(t *testing.T) {

	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{}

	tx := Txn{}
	var blockNumber hexutil.Big
	blockNumber.ToInt().SetInt64(10)
	tx.Receipt.BlockNumber = &blockNumber

	isMined, err := tx.GetTXReceipt(context.Background(), &r)

	assert.Equal(nil, err)
	assert.Equal("eth_getTransactionReceipt", r.capturedMethod)
	assert.Equal(true, isMined)
}

func TestGetTXReceiptNotMined(t *testing.T) {

	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{}

	tx := Txn{}
	var blockNumber hexutil.Big
	tx.Receipt.BlockNumber = &blockNumber

	isMined, err := tx.GetTXReceipt(context.Background(), &r)

	assert.Equal(nil, err)
	assert.Equal("eth_getTransactionReceipt", r.capturedMethod)
	assert.Equal(false, isMined)
}

func TestGetTXReceiptFail(t *testing.T) {

	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{
		mockError: fmt.Errorf("pop"),
	}

	tx := Txn{}
	var blockNumber hexutil.Big
	tx.Receipt.BlockNumber = &blockNumber

	isMined, err := tx.GetTXReceipt(context.Background(), &r)

	assert.EqualError(err, "eth_getTransactionReceipt returned: pop")
	assert.Equal("eth_getTransactionReceipt", r.capturedMethod)
	assert.Equal(false, isMined)
}

func TestGetTXReceiptOrionTX(t *testing.T) {

	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{}

	tx := Txn{
		PrivacyGroupID: "test",
		PrivateFrom:    "foo",
	}
	var blockNumber hexutil.Big
	blockNumber.ToInt().SetInt64(10)
	tx.Receipt.BlockNumber = &blockNumber

	isMined, err := tx.GetTXReceipt(context.Background(), &r)

	assert.Equal(nil, err)
	assert.Equal("eth_getTransactionReceipt", r.capturedMethod)
	assert.Equal("priv_getTransactionReceipt", r.capturedMethod2)
	assert.Equal(true, isMined)
}

func TestGetTXReceiptOrionTXFail(t *testing.T) {

	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{
		mockError2: fmt.Errorf("pop"),
	}

	tx := Txn{
		PrivacyGroupID: "test",
		PrivateFrom:    "foo",
	}
	var blockNumber hexutil.Big
	blockNumber.ToInt().SetInt64(10)
	tx.Receipt.BlockNumber = &blockNumber

	isMined, err := tx.GetTXReceipt(context.Background(), &r)

	assert.EqualError(err, "priv_getTransactionReceipt returned: pop")
	assert.Equal("eth_getTransactionReceipt", r.capturedMethod)
	assert.Equal("priv_getTransactionReceipt", r.capturedMethod2)
	assert.Equal(false, isMined)
}
