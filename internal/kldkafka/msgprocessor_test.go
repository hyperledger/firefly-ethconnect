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

package kldkafka

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type errorReply struct {
	status int
	err    error
}

type testMsgContext struct {
	jsonMsg     string
	badMsgType  string
	replies     []kldmessages.ReplyWithHeaders
	errorRepies []*errorReply
}

type testRPC struct {
	ethSendTransactionResult       string
	ethSendTransactionErr          error
	ethGetTransactionCountResult   hexutil.Uint64
	ethGetTransactionCountErr      error
	ethGetTransactionReceiptResult kldeth.TxnReceipt
	ethGetTransactionReceiptErr    error
	calls                          []string
}

const testFromAddr = "0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1"

var goodDeployTxnJSON = "{" +
	"  \"headers\":{\"type\": \"DeployContract\"}," +
	"  \"solidity\":\"pragma solidity ^0.4.17; contract t {constructor() public {}}\"," +
	"  \"from\":\"" + testFromAddr + "\"," +
	"  \"nonce\":\"123\"," +
	"  \"gas\":\"123\"" +
	"}"

var goodSendTxnJSON = "{" +
	"  \"headers\":{\"type\": \"SendTransaction\"}," +
	"  \"from\":\"" + testFromAddr + "\"," +
	"  \"nonce\":\"123\"," +
	"  \"gas\":\"123\"," +
	"  \"method\":{\"name\":\"test\"}" +
	"}"

func (r *testRPC) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	r.calls = append(r.calls, method)
	if method == "eth_sendTransaction" {
		result = r.ethSendTransactionResult
		return r.ethSendTransactionErr
	} else if method == "eth_getTransactionCount" {
		result = r.ethGetTransactionCountResult
		return r.ethGetTransactionCountErr
	} else if method == "eth_getTransactionReceipt" {
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(r.ethGetTransactionReceiptResult))
		return r.ethGetTransactionReceiptErr
	}
	panic(fmt.Errorf("method unknown to test: %s", method))
}

func (c *testMsgContext) String() string {
	return "<testmessage>"
}

func (c *testMsgContext) Headers() *kldmessages.CommonHeaders {
	commonMsg := kldmessages.RequestCommon{}
	if c.badMsgType != "" {
		commonMsg.Headers.MsgType = c.badMsgType
	} else if err := c.Unmarshal(&commonMsg); err != nil {
		panic(fmt.Errorf("Unable to unmarshal test message: %s", c.jsonMsg))
	}
	log.Infof("Test message headers: %+v", commonMsg.Headers)
	return &commonMsg.Headers
}

func (c *testMsgContext) Unmarshal(msg interface{}) error {
	log.Infof("Unmarshaling test message: %s", c.jsonMsg)
	return json.Unmarshal([]byte(c.jsonMsg), msg)
}

func (c *testMsgContext) SendErrorReply(status int, err error) {
	log.Infof("Sending error reply. Status=%d Err=%s", status, err)
	c.errorRepies = append(c.errorRepies, &errorReply{
		status: status,
		err:    err,
	})
}

func (c *testMsgContext) Reply(replyMsg kldmessages.ReplyWithHeaders) {
	log.Infof("Sending success reply: %s", replyMsg.ReplyHeaders().MsgType)
	c.replies = append(c.replies, replyMsg)
}

func TestOnMessageBadMessage(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"badness\"}" +
		"}"
	msgProcessor.OnMessage(testMsgContext)

	assert.Empty(testMsgContext.replies)
	assert.NotEmpty(testMsgContext.errorRepies)
	assert.Equal(400, testMsgContext.errorRepies[0].status)
	assert.Regexp("Unknown message type", testMsgContext.errorRepies[0].err.Error())
}

func TestOnDeployContractMessageBadMsg(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"DeployContract\"}," +
		"  \"nonce\":\"123\"," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"" +
		"}"
	msgProcessor.OnMessage(testMsgContext)

	assert.NotEmpty(testMsgContext.errorRepies)
	assert.Empty(testMsgContext.replies)
	assert.Regexp("empty source string", testMsgContext.errorRepies[0].err.Error())

}
func TestOnDeployContractMessageBadJSON(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "badness"
	testMsgContext.badMsgType = kldmessages.MsgTypeDeployContract
	msgProcessor.OnMessage(testMsgContext)

	assert.NotEmpty(testMsgContext.errorRepies)
	assert.Empty(testMsgContext.replies)
	assert.Regexp("invalid character", testMsgContext.errorRepies[0].err.Error())

}
func TestOnDeployContractMessageGoodTxnErrOnReceipt(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = goodDeployTxnJSON
	testRPC := &testRPC{
		ethSendTransactionResult:    "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
		ethGetTransactionReceiptErr: fmt.Errorf("pop"),
	}
	msgProcessor.Init(testRPC, 1)                       // configured in seconds for real world
	msgProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	msgProcessor.OnMessage(testMsgContext)
	txnWG := &msgProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	assert.Equal(1, len(testMsgContext.errorRepies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	assert.Regexp("Error obtaining transaction receipt", testMsgContext.errorRepies[0].err.Error())

}

func goodMessageRPC() *testRPC {
	blockHash := common.HexToHash("0x6e710868fd2d0ac1f141ba3f0cd569e38ce1999d8f39518ee7633d2b9a7122af")
	blockNumber := hexutil.Big(*big.NewInt(12345))
	contractAddr := common.HexToAddress("0x28a62Cb478a3c3d4DAAD84F1148ea16cd1A66F37")
	cumulativeGasUsed := hexutil.Big(*big.NewInt(23456))
	fromAddr := common.HexToAddress("0xBa25be62a5C55d4ad1d5520268806A8730A4DE5E")
	gasUsed := hexutil.Big(*big.NewInt(345678))
	status := hexutil.Big(*big.NewInt(1))
	toAddr := common.HexToAddress("0xD7FAC2bCe408Ed7C6ded07a32038b1F79C2b27d3")
	transactionHash := common.HexToHash("0xe2215336b09f9b5b82e36e1144ed64f40a42e61b68fdaca82549fd98b8531a89")
	transactionIndex := hexutil.Uint(456789)
	testRPC := &testRPC{
		ethSendTransactionResult: transactionHash.String(),
		ethGetTransactionReceiptResult: kldeth.TxnReceipt{
			BlockHash:         &blockHash,
			BlockNumber:       &blockNumber,
			ContractAddress:   &contractAddr,
			CumulativeGasUsed: &cumulativeGasUsed,
			From:              &fromAddr,
			GasUsed:           &gasUsed,
			Status:            &status,
			To:                &toAddr,
			TransactionHash:   &transactionHash,
			TransactionIndex:  &transactionIndex,
		},
	}
	return testRPC
}

func TestOnDeployContractMessageGoodTxnMined(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = goodDeployTxnJSON

	testRPC := goodMessageRPC()
	msgProcessor.Init(testRPC, 1)                       // configured in seconds for real world
	msgProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	msgProcessor.OnMessage(testMsgContext)
	txnWG := &msgProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	assert.Equal(0, len(testMsgContext.errorRepies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	replyMsg := testMsgContext.replies[0]
	assert.Equal("TransactionSuccess", replyMsg.ReplyHeaders().MsgType)
	replyMsgBytes, _ := json.Marshal(&replyMsg)
	var replyMsgMap map[string]interface{}
	json.Unmarshal(replyMsgBytes, &replyMsgMap)

	assert.Equal("0x6e710868fd2d0ac1f141ba3f0cd569e38ce1999d8f39518ee7633d2b9a7122af", replyMsgMap["blockHash"])
	assert.Equal("12345", replyMsgMap["blockNumber"])
	assert.Equal("0x3039", replyMsgMap["blockNumberHex"])
	assert.Equal("0x28a62cb478a3c3d4daad84f1148ea16cd1a66f37", replyMsgMap["contractAddress"])
	assert.Equal("23456", replyMsgMap["cumulativeGasUsed"])
	assert.Equal("0x5ba0", replyMsgMap["cumulativeGasUsedHex"])
	assert.Equal("0xba25be62a5c55d4ad1d5520268806a8730a4de5e", replyMsgMap["from"])
	assert.Equal("345678", replyMsgMap["gasUsed"])
	assert.Equal("0x5464e", replyMsgMap["gasUsedHex"])
	assert.Equal("123", replyMsgMap["nonce"])
	assert.Equal("0x7b", replyMsgMap["nonceHex"])
	assert.Equal("1", replyMsgMap["status"])
	assert.Equal("0x1", replyMsgMap["statusHex"])
	assert.Equal("0xd7fac2bce408ed7c6ded07a32038b1f79c2b27d3", replyMsgMap["to"])
	assert.Equal("456789", replyMsgMap["transactionIndex"])
	assert.Equal("0x6f855", replyMsgMap["transactionIndexHex"])
}

func TestOnDeployContractMessageFailedTxnMined(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = goodDeployTxnJSON

	testRPC := goodMessageRPC()
	failStatus := hexutil.Big(*big.NewInt(0))
	testRPC.ethGetTransactionReceiptResult.Status = &failStatus
	msgProcessor.Init(testRPC, 1)                       // configured in seconds for real world
	msgProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	msgProcessor.OnMessage(testMsgContext)
	txnWG := &msgProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	replyMsg := testMsgContext.replies[0]
	assert.Equal("TransactionFailure", replyMsg.ReplyHeaders().MsgType)
}

func TestOnDeployContractMessageFailedTxn(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = goodDeployTxnJSON
	testRPC := &testRPC{
		ethSendTransactionErr: fmt.Errorf("fizzle"),
	}
	msgProcessor.Init(testRPC, 5000)

	msgProcessor.OnMessage(testMsgContext)

	assert.Equal("fizzle", testMsgContext.errorRepies[0].err.Error())
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}

func TestOnDeployContractMessageFailedToGetNonce(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"DeployContract\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"" +
		"}"
	testRPC := &testRPC{
		ethGetTransactionCountErr: fmt.Errorf("ding"),
	}
	msgProcessor.Init(testRPC, 1)

	msgProcessor.OnMessage(testMsgContext)

	assert.Equal("ding", testMsgContext.errorRepies[0].err.Error())
	assert.EqualValues([]string{"eth_getTransactionCount"}, testRPC.calls)
}

func TestOnSendTransactionMessageMissingFrom(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"nonce\":\"123\"" +
		"}"
	msgProcessor.OnMessage(testMsgContext)

	assert.NotEmpty(testMsgContext.errorRepies)
	assert.Empty(testMsgContext.replies)
	assert.Regexp("'from' must be supplied", testMsgContext.errorRepies[0].err.Error())

}

func TestOnSendTransactionMessageBadNonce(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"nonce\":\"abc\"" +
		"}"
	msgProcessor.OnMessage(testMsgContext)

	assert.NotEmpty(testMsgContext.errorRepies)
	assert.Empty(testMsgContext.replies)
	assert.Regexp("Converting supplied 'nonce' to integer", testMsgContext.errorRepies[0].err.Error())

}

func TestOnSendTransactionMessageBadMsg(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"nonce\":\"123\"," +
		"  \"value\":\"abc\"," +
		"  \"method\":{\"name\":\"test\"}" +
		"}"
	msgProcessor.OnMessage(testMsgContext)

	assert.NotEmpty(testMsgContext.errorRepies)
	assert.Empty(testMsgContext.replies)
	assert.Regexp("Converting supplied 'value' to big integer", testMsgContext.errorRepies[0].err.Error())

}

func TestOnSendTransactionMessageBadJSON(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "badness"
	testMsgContext.badMsgType = kldmessages.MsgTypeSendTransaction
	msgProcessor.OnMessage(testMsgContext)

	assert.NotEmpty(testMsgContext.errorRepies)
	assert.Empty(testMsgContext.replies)
	assert.Regexp("invalid character", testMsgContext.errorRepies[0].err.Error())

}
func TestOnSendTransactionMessageTxnTimeout(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = goodSendTxnJSON
	testRPC := &testRPC{
		ethSendTransactionResult: "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
	}
	msgProcessor.Init(testRPC, 1)                       // configured in seconds for real world
	msgProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	msgProcessor.OnMessage(testMsgContext)
	txnWG := &msgProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg
	txnWG.Wait()
	assert.Equal(1, len(testMsgContext.errorRepies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	assert.Regexp("Timed out waiting for transaction receipt", testMsgContext.errorRepies[0].err.Error())

}

func TestOnSendTransactionMessageFailedTxn(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = goodSendTxnJSON
	testRPC := &testRPC{
		ethSendTransactionErr: fmt.Errorf("pop"),
	}
	msgProcessor.Init(testRPC, 1)

	msgProcessor.OnMessage(testMsgContext)

	assert.Equal("pop", testMsgContext.errorRepies[0].err.Error())
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}

func TestOnSendTransactionMessageFailedToGetNonce(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"" +
		"}"
	testRPC := &testRPC{
		ethGetTransactionCountErr: fmt.Errorf("poof"),
	}
	msgProcessor.Init(testRPC, 1)

	msgProcessor.OnMessage(testMsgContext)

	assert.Equal("poof", testMsgContext.errorRepies[0].err.Error())
	assert.EqualValues([]string{"eth_getTransactionCount"}, testRPC.calls)
}

func TestOnSendTransactionMessageInflightNonce(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	msgProcessor.inflightTxns["0x83dbc8e329b38cba0fc4ed99b1ce9c2a390abdc1"] =
		[]*inflightTxn{&inflightTxn{nonce: 100}, &inflightTxn{nonce: 101}}
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"gas\":\"123\"," +
		"  \"method\":{\"name\":\"test\"}" +
		"}"
	testRPC := &testRPC{
		ethSendTransactionResult: "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
	}
	msgProcessor.Init(testRPC, 1)

	msgProcessor.OnMessage(testMsgContext)

	assert.Empty(testMsgContext.errorRepies)
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}
