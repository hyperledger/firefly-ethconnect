// Copyright 2018 Kaleido, a ConsenSys business

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

	isMined, err := tx.GetTXReceipt(&r)

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

	isMined, err := tx.GetTXReceipt(&r)

	assert.Equal(nil, err)
	assert.Equal("eth_getTransactionReceipt", r.capturedMethod)
	assert.Equal(false, isMined)
}
