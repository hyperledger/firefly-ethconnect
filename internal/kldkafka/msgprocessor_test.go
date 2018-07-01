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
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
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
	ethSendTransactionResult     string
	ethSendTransactionErr        error
	ethGetTransactionCountResult hexutil.Uint64
	ethGetTransactionCountErr    error
	calls                        []string
}

var goodDeployTxnJSON = "{" +
	"  \"headers\":{\"type\": \"DeployContract\"}," +
	"  \"solidity\":\"pragma solidity ^0.4.17; contract t {constructor() public {}}\"," +
	"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
	"  \"nonce\":\"123\"," +
	"  \"gas\":\"123\"" +
	"}"

var goodSendTxnJSON = "{" +
	"  \"headers\":{\"type\": \"SendTransaction\"}," +
	"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
	"  \"nonce\":\"123\"," +
	"  \"gas\":\"123\"" +
	"}"

func (r *testRPC) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	r.calls = append(r.calls, method)
	if method == "eth_sendTransaction" {
		result = r.ethSendTransactionResult
		return r.ethSendTransactionErr
	} else if method == "eth_getTransactionCount" {
		result = r.ethGetTransactionCountResult
		return r.ethGetTransactionCountErr
	} else {
		panic(fmt.Errorf("method != eth_sendTransaction: %s", method))
	}
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
func TestOnDeployContractMessageGoodTxn(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = goodDeployTxnJSON
	testRPC := &testRPC{
		ethSendTransactionResult: "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
	}
	msgProcessor.SetRPC(testRPC)

	msgProcessor.OnMessage(testMsgContext)

	assert.Empty(testMsgContext.errorRepies)
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}

func TestOnDeployContractMessageFailedTxn(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = goodDeployTxnJSON
	testRPC := &testRPC{
		ethSendTransactionErr: fmt.Errorf("fizzle"),
	}
	msgProcessor.SetRPC(testRPC)

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
	msgProcessor.SetRPC(testRPC)

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
		"  \"value\":\"abc\"" +
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
func TestOnSendTransactionMessageGoodTxn(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = goodSendTxnJSON
	testRPC := &testRPC{
		ethSendTransactionResult: "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
	}
	msgProcessor.SetRPC(testRPC)

	msgProcessor.OnMessage(testMsgContext)

	assert.Empty(testMsgContext.errorRepies)
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}

func TestOnSendTransactionMessageFailedTxn(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := newMsgProcessor()
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = goodSendTxnJSON
	testRPC := &testRPC{
		ethSendTransactionErr: fmt.Errorf("pop"),
	}
	msgProcessor.SetRPC(testRPC)

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
	msgProcessor.SetRPC(testRPC)

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
		"  \"gas\":\"123\"" +
		"}"
	testRPC := &testRPC{
		ethSendTransactionResult: "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
	}
	msgProcessor.SetRPC(testRPC)

	msgProcessor.OnMessage(testMsgContext)

	assert.Empty(testMsgContext.errorRepies)
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}
