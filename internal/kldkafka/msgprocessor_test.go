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
	"encoding/json"
	"fmt"
	"testing"

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
	log.Infof("Sending error reply. Status=%d", status)
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

	msgProcessor := &msgProcessor{}
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

	msgProcessor := &msgProcessor{}
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"DeployContract\"}" +
		"}"
	msgProcessor.OnMessage(testMsgContext)

	assert.NotEmpty(testMsgContext.errorRepies)
	assert.Empty(testMsgContext.replies)
	assert.Regexp("empty source string", testMsgContext.errorRepies[0].err.Error())

}
func TestOnDeployContractMessageBadJSON(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := &msgProcessor{}
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "badness"
	testMsgContext.badMsgType = kldmessages.MsgTypeDeployContract
	msgProcessor.OnMessage(testMsgContext)

	assert.NotEmpty(testMsgContext.errorRepies)
	assert.Empty(testMsgContext.replies)
	assert.Regexp("invalid character", testMsgContext.errorRepies[0].err.Error())

}
func TestOnDeployContractMessageGoodMsg(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := &msgProcessor{}
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"DeployContract\"}," +
		"  \"solidity\":\"pragma solidity ^0.4.17; contract t {constructor() public {}}\"," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"nonce\":\"123\"," +
		"  \"gas\":\"123\"" +
		"}"
	msgProcessor.OnMessage(testMsgContext)

	assert.Empty(testMsgContext.errorRepies)
}

func TestOnSendTransactionMessageBadMsg(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := &msgProcessor{}
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}" +
		"}"
	msgProcessor.OnMessage(testMsgContext)

	assert.NotEmpty(testMsgContext.errorRepies)
	assert.Empty(testMsgContext.replies)
	assert.Regexp("not a valid hex address", testMsgContext.errorRepies[0].err.Error())

}

func TestOnSendTransactionMessageBadJSON(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := &msgProcessor{}
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "badness"
	testMsgContext.badMsgType = kldmessages.MsgTypeSendTransaction
	msgProcessor.OnMessage(testMsgContext)

	assert.NotEmpty(testMsgContext.errorRepies)
	assert.Empty(testMsgContext.replies)
	assert.Regexp("invalid character", testMsgContext.errorRepies[0].err.Error())

}
func TestOnSendTransactionMessageGoodMsg(t *testing.T) {
	assert := assert.New(t)

	msgProcessor := &msgProcessor{}
	testMsgContext := &testMsgContext{}
	testMsgContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"nonce\":\"123\"," +
		"  \"gas\":\"123\"" +
		"}"
	msgProcessor.OnMessage(testMsgContext)

	assert.Empty(testMsgContext.errorRepies)
}
