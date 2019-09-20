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

package kldmessages

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSONEncodingAssumptions(t *testing.T) {
	assert := assert.New(t)

	jsonMsg := "{" +
		"\"headers\":{" +
		"  \"id\":\"test123\"," +
		"  \"type\":\"SendTransaction\"," +
		"  \"ctx\":{" +
		"    \"myContext\":\"hello world\"" +
		"  }" +
		"}," +
		"\"nonce\":123,\"from\":\"0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c\"," +
		"\"value\":0,\"gas\":456,\"gasPrice\":789," +
		"\"params\":[\"123\",123,\"abc\",\"0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c\"]," +
		"\"to\":\"0x2b8c0ECc76d0759a8F50b2E14A6881367D805832\"," +
		"\"function\":{\"name\":\"testFunc\",\"constant\":false,\"stateMutability\":\"nonpayable\"," +
		"\"inputs\":[" +
		"  {\"name\":\"param1\",\"type\":\"uint8\"}," +
		"  {\"name\":\"param2\",\"type\":\"int256\"}," +
		"  {\"name\":\"param3\",\"type\":\"string\"}," +
		"  {\"name\":\"param4\",\"type\":\"address\"}" +
		"]," +
		"\"outputs\":[{\"name\":\"ret1\",\"type\":\"uint256\"}]}" +
		"}"
	var sendTxnMsg SendTransaction
	err := json.Unmarshal([]byte(jsonMsg), &sendTxnMsg)

	assert.Nil(err)
	assert.Equal("123", sendTxnMsg.Parameters[0])
	assert.Equal(float64(123), sendTxnMsg.Parameters[1]) // JSON numbers go to floats
	assert.Equal("abc", sendTxnMsg.Parameters[2])
	assert.Equal("0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c", sendTxnMsg.Parameters[3])
	ctx := sendTxnMsg.Headers.Context
	assert.Equal("hello world", ctx["myContext"])

	// Simulate an error for this transaction, and check it marshals/unmarshals with the embedded request payload
	exampleErrMsg := NewErrorReply(fmt.Errorf("pop"), &sendTxnMsg)
	marshaledErrMsg, _ := json.Marshal(&exampleErrMsg)
	var unmarshaledErrMsg ErrorReply
	json.Unmarshal(marshaledErrMsg, &unmarshaledErrMsg)
	assert.Equal(MsgTypeError, unmarshaledErrMsg.ReplyHeaders().MsgType)
	assert.Equal("pop", unmarshaledErrMsg.ErrorMessage)
	assert.NotEmpty(unmarshaledErrMsg.OriginalMessage)

}

func TestErrorMessageForEmptyData(t *testing.T) {
	assert := assert.New(t)

	exampleErrMsg := NewErrorReply(fmt.Errorf("pop"), []byte{})
	marshaledErrMsg, _ := json.Marshal(&exampleErrMsg)
	var unmarshaledErrMsg ErrorReply
	json.Unmarshal(marshaledErrMsg, &unmarshaledErrMsg)
	assert.Equal("pop", unmarshaledErrMsg.ErrorMessage)
	assert.Equal("", unmarshaledErrMsg.OriginalMessage)
}

func TestErrorMessageForUnparsableBinaryData(t *testing.T) {
	assert := assert.New(t)

	exampleErrMsg := NewErrorReply(fmt.Errorf("pop"), []byte{00, 0xfe, 0xed, 0xbe, 0xef})
	marshaledErrMsg, _ := json.Marshal(&exampleErrMsg)
	var unmarshaledErrMsg ErrorReply
	json.Unmarshal(marshaledErrMsg, &unmarshaledErrMsg)
	assert.Equal("pop", unmarshaledErrMsg.ErrorMessage)
	assert.Equal("\u0000\ufffd\ufffd\ufffd\ufffd", unmarshaledErrMsg.OriginalMessage)
}

func TestIsReceiptForReceipt(t *testing.T) {
	assert := assert.New(t)
	var m ReplyWithHeaders
	m = &TransactionReceipt{}
	assert.NotNil(m.IsReceipt())
}

func TestIsReceiptForError(t *testing.T) {
	assert := assert.New(t)
	var m ReplyWithHeaders
	m = &ErrorReply{}
	assert.Nil(m.IsReceipt())
}
