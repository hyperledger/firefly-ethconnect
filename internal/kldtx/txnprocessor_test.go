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

package kldtx

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ecrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/julienschmidt/httprouter"
	"github.com/spf13/cobra"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type errorReply struct {
	status             int
	err                error
	txHash             string
	gapFillTxHash      string
	gapFillTxSucceeded bool
}

type testTxnContext struct {
	jsonMsg      string
	badMsgType   string
	replies      []kldmessages.ReplyWithHeaders
	errorReplies []*errorReply
}

type testRPC struct {
	ethSendTransactionResult       string
	ethSendTransactionErr          error
	ethSendTransactionErrOnce      bool
	ethSendTransactionCond         *sync.Cond
	ethSendTransactionReady        bool
	ethSendTransactionFirstCond    *sync.Cond
	ethSendTransactionFirstReady   bool
	ethGetTransactionCountResult   hexutil.Uint64
	ethGetTransactionCountErr      error
	ethGetTransactionReceiptResult kldeth.TxnReceipt
	ethGetTransactionReceiptErr    error
	privFindPrivacyGroupResult     []kldeth.OrionPrivacyGroup
	privFindPrivacyGroupErr        error
	condLock                       sync.Mutex
	calls                          []string
	params                         [][]interface{}
}

const testFromAddr = "0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1"

var goodDeployTxnJSON = "{" +
	"  \"headers\":{\"type\": \"DeployContract\"}," +
	"  \"solidity\":\"pragma solidity >=0.4.22 <0.6.0; contract t {constructor() public {}}\"," +
	"  \"from\":\"" + testFromAddr + "\"," +
	"  \"nonce\":\"123\"," +
	"  \"gas\":\"123\"" +
	"}"

var goodHDWalletDeployTxnJSON = "{" +
	"  \"headers\":{\"type\": \"DeployContract\"}," +
	"  \"solidity\":\"pragma solidity >=0.4.22 <0.6.0; contract t {constructor() public {}}\"," +
	"  \"from\":\"hd-testinst-testwallet-1234\"," +
	"  \"nonce\":\"123\"," +
	"  \"gas\":\"123\"" +
	"}"

var goodSendTxnJSON = "{" +
	"  \"headers\":{\"type\": \"SendTransaction\"}," +
	"  \"from\":\"" + testFromAddr + "\"," +
	"  \"gas\":\"123\"," +
	"  \"method\":{\"name\":\"test\"}" +
	"}"

var goodDeployTxnPrivateJSON = "{" +
	"  \"headers\":{\"type\": \"DeployContract\"}," +
	"  \"solidity\":\"pragma solidity >=0.4.22 <0.6.0; contract t {constructor() public {}}\"," +
	"  \"from\":\"" + testFromAddr + "\"," +
	"  \"nonce\":\"123\"," +
	"  \"gas\":\"123\"," +
	"  \"privateFrom\":\"s6a3mQ8IvrI2ZgHqHZlJaELiJs10HxlZNIwNd669FH4=\"," +
	"  \"privateFor\":[\"oD76ZRgu6py/WKrsXbtF9P2Mf1mxVxzqficE1Uiw6S8=\"]" +
	"}"

func (r *testRPC) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	r.calls = append(r.calls, method)
	r.params = append(r.params, args)
	if method == "eth_sendTransaction" || method == "eea_sendTransaction" {
		r.condLock.Lock()
		sendTX := args[0].(*kldeth.SendTXArgs)
		isFirst := (sendTX.Nonce != nil && uint64(*sendTX.Nonce) == 0 && len(*sendTX.Data) > 0)
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(r.ethSendTransactionResult))
		if isFirst && r.ethSendTransactionFirstCond != nil {
			for !r.ethSendTransactionFirstReady {
				r.ethSendTransactionFirstCond.Wait()
			}
		} else if r.ethSendTransactionCond != nil {
			for !r.ethSendTransactionReady {
				r.ethSendTransactionCond.Wait()
			}
		}
		r.condLock.Unlock()
		if !r.ethSendTransactionErrOnce || isFirst {
			return r.ethSendTransactionErr
		}
		return nil
	} else if method == "eth_sendRawTransaction" {
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(r.ethSendTransactionResult))
		return r.ethSendTransactionErr
	} else if method == "eth_getTransactionCount" || method == "priv_getTransactionCount" {
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(r.ethGetTransactionCountResult))
		return r.ethGetTransactionCountErr
	} else if method == "priv_findPrivacyGroup" {
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(r.privFindPrivacyGroupResult))
		return r.privFindPrivacyGroupErr
	} else if method == "eth_getTransactionReceipt" {
		reflect.ValueOf(result).Elem().Set(reflect.ValueOf(r.ethGetTransactionReceiptResult))
		return r.ethGetTransactionReceiptErr
	}
	panic(fmt.Errorf("method unknown to test: %s", method))
}

func (c *testTxnContext) Context() context.Context {
	return context.Background()
}

func (c *testTxnContext) String() string {
	return "<testmessage>"
}

func (c *testTxnContext) Headers() *kldmessages.CommonHeaders {
	commonMsg := kldmessages.RequestCommon{}
	if c.badMsgType != "" {
		commonMsg.Headers.MsgType = c.badMsgType
	} else if err := c.Unmarshal(&commonMsg); err != nil {
		panic(fmt.Errorf("Unable to unmarshal test message: %s", c.jsonMsg))
	}
	log.Infof("Test message headers: %+v", commonMsg.Headers)
	return &commonMsg.Headers.CommonHeaders
}

func (c *testTxnContext) Unmarshal(msg interface{}) error {
	log.Infof("Unmarshaling test message: %s", c.jsonMsg)
	return json.Unmarshal([]byte(c.jsonMsg), msg)
}

func (c *testTxnContext) SendErrorReply(status int, err error) {
	c.SendErrorReplyWithTX(status, err, "")
}

func (c *testTxnContext) SendErrorReplyWithGapFill(status int, err error, gapFillTxHash string, gapFillSucceeded bool) {
	log.Infof("Sending error reply. Status=%d Err=%s GapTX? '%s' GapOK? %t", status, err, gapFillTxHash, gapFillSucceeded)
	c.errorReplies = append(c.errorReplies, &errorReply{
		status:             status,
		err:                err,
		gapFillTxHash:      gapFillTxHash,
		gapFillTxSucceeded: gapFillSucceeded,
	})
}

func (c *testTxnContext) SendErrorReplyWithTX(status int, err error, txHash string) {
	log.Infof("Sending error reply. Status=%d Err=%s", status, err)
	c.errorReplies = append(c.errorReplies, &errorReply{
		status: status,
		err:    err,
		txHash: txHash,
	})
}

func (c *testTxnContext) Reply(replyMsg kldmessages.ReplyWithHeaders) {
	log.Infof("Sending success reply: %s", replyMsg.ReplyHeaders().MsgType)
	c.replies = append(c.replies, replyMsg)
}

func TestOnMessageBadMessage(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"badness\"}" +
		"}"
	txnProcessor.OnMessage(testTxnContext)

	assert.Empty(testTxnContext.replies)
	assert.NotEmpty(testTxnContext.errorReplies)
	assert.Equal(400, testTxnContext.errorReplies[0].status)
	assert.Regexp("Unknown message type", testTxnContext.errorReplies[0].err.Error())
}

func TestOnDeployContractMessageBadMsg(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"DeployContract\"}," +
		"  \"nonce\":\"123\"," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"" +
		"}"
	txnProcessor.OnMessage(testTxnContext)

	assert.NotEmpty(testTxnContext.errorReplies)
	assert.Empty(testTxnContext.replies)
	assert.Equal("Missing Compiled Code + ABI, or Solidity", testTxnContext.errorReplies[0].err.Error())

}
func TestOnDeployContractMessageBadJSON(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "badness"
	testTxnContext.badMsgType = kldmessages.MsgTypeDeployContract
	txnProcessor.OnMessage(testTxnContext)

	assert.NotEmpty(testTxnContext.errorReplies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("invalid character", testTxnContext.errorReplies[0].err.Error())

}
func TestOnDeployContractMessageGoodTxnErrOnReceipt(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnJSON
	testRPC := &testRPC{
		ethSendTransactionResult:    "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
		ethGetTransactionReceiptErr: fmt.Errorf("pop"),
	}
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	for inMap := false; !inMap; _, inMap = txnProcessor.inflightTxns[strings.ToLower(testFromAddr)] {
		time.Sleep(1 * time.Millisecond)
	}
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	assert.Equal(1, len(testTxnContext.errorReplies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	assert.Regexp("Error obtaining transaction receipt", testTxnContext.errorReplies[0].err.Error())

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

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnJSON

	testRPC := goodMessageRPC()
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	for inMap := false; !inMap; _, inMap = txnProcessor.inflightTxns[strings.ToLower(testFromAddr)] {
		time.Sleep(1 * time.Millisecond)
	}
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	assert.Equal(0, len(testTxnContext.errorReplies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	replyMsg := testTxnContext.replies[0]
	assert.Equal("TransactionSuccess", replyMsg.ReplyHeaders().MsgType)
	replyMsgBytes, _ := json.Marshal(&replyMsg)
	var replyMsgMap map[string]interface{}
	json.Unmarshal(replyMsgBytes, &replyMsgMap)

	assert.Equal("0x6e710868fd2d0ac1f141ba3f0cd569e38ce1999d8f39518ee7633d2b9a7122af", replyMsgMap["blockHash"])
	assert.Equal("12345", replyMsgMap["blockNumber"])
	assert.Equal("0x28a62cb478a3c3d4daad84f1148ea16cd1a66f37", replyMsgMap["contractAddress"])
	assert.Equal("23456", replyMsgMap["cumulativeGasUsed"])
	assert.Equal("0xba25be62a5c55d4ad1d5520268806a8730a4de5e", replyMsgMap["from"])
	assert.Equal("345678", replyMsgMap["gasUsed"])
	assert.Equal("123", replyMsgMap["nonce"])
	assert.Equal("1", replyMsgMap["status"])
	assert.Equal("0xd7fac2bce408ed7c6ded07a32038b1f79c2b27d3", replyMsgMap["to"])
	assert.Equal("456789", replyMsgMap["transactionIndex"])
}

func TestOnDeployContractMessageGoodTxnMinedHDWallet(t *testing.T) {
	assert := assert.New(t)

	key, _ := ecrypto.GenerateKey()
	addr := ecrypto.PubkeyToAddress(key.PublicKey)
	svr := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(200)
		res.Write([]byte(`
    {
      "address": "` + addr.String() + `",
      "privateKey": "` + hex.EncodeToString(ecrypto.FromECDSA(key)) + `"
    }`))
	}))
	defer svr.Close()

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
		HDWalletConf: HDWalletConf{
			URLTemplate: svr.URL,
		},
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodHDWalletDeployTxnJSON

	testRPC := goodMessageRPC()
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	for inMap := false; !inMap; _, inMap = txnProcessor.inflightTxns[strings.ToLower(addr.String())] {
		time.Sleep(1 * time.Millisecond)
	}
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(addr.String())][0].wg

	txnWG.Wait()
	assert.Equal(0, len(testTxnContext.errorReplies))

	assert.Equal("eth_sendRawTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])
	assert.Regexp("^0x[0-9a-z]+$", testRPC.params[0][0].(string))

	replyMsg := testTxnContext.replies[0]
	assert.Equal("TransactionSuccess", replyMsg.ReplyHeaders().MsgType)
	replyMsgBytes, _ := json.Marshal(&replyMsg)
	var replyMsgMap map[string]interface{}
	json.Unmarshal(replyMsgBytes, &replyMsgMap)

	assert.Equal("0x6e710868fd2d0ac1f141ba3f0cd569e38ce1999d8f39518ee7633d2b9a7122af", replyMsgMap["blockHash"])
	assert.Equal("12345", replyMsgMap["blockNumber"])
	assert.Equal("0x28a62cb478a3c3d4daad84f1148ea16cd1a66f37", replyMsgMap["contractAddress"])
	assert.Equal("23456", replyMsgMap["cumulativeGasUsed"])
	assert.Equal("0xba25be62a5c55d4ad1d5520268806a8730a4de5e", replyMsgMap["from"])
	assert.Equal("345678", replyMsgMap["gasUsed"])
	assert.Equal("123", replyMsgMap["nonce"])
	assert.Equal("1", replyMsgMap["status"])
	assert.Equal("0xd7fac2bce408ed7c6ded07a32038b1f79c2b27d3", replyMsgMap["to"])
	assert.Equal("456789", replyMsgMap["transactionIndex"])
}

func TestOnDeployContractPrivateMessageGoodTxnMined(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnPrivateJSON

	testRPC := goodMessageRPC()
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	for inMap := false; !inMap; _, inMap = txnProcessor.inflightTxns[strings.ToLower(testFromAddr)] {
		time.Sleep(1 * time.Millisecond)
	}
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	assert.Equal(0, len(testTxnContext.errorReplies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	sendTxArg0JSON, _ := json.Marshal(testRPC.params[0][0])
	var sendTxArg0Generic map[string]interface{}
	json.Unmarshal(sendTxArg0JSON, &sendTxArg0Generic)
	assert.Equal("s6a3mQ8IvrI2ZgHqHZlJaELiJs10HxlZNIwNd669FH4=", sendTxArg0Generic["privateFrom"])
	assert.Equal("oD76ZRgu6py/WKrsXbtF9P2Mf1mxVxzqficE1Uiw6S8=", sendTxArg0Generic["privateFor"].([]interface{})[0])

	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	replyMsg := testTxnContext.replies[0]
	assert.Equal("TransactionSuccess", replyMsg.ReplyHeaders().MsgType)
	replyMsgBytes, _ := json.Marshal(&replyMsg)
	var replyMsgMap map[string]interface{}
	json.Unmarshal(replyMsgBytes, &replyMsgMap)

	assert.Equal("0x6e710868fd2d0ac1f141ba3f0cd569e38ce1999d8f39518ee7633d2b9a7122af", replyMsgMap["blockHash"])
	assert.Equal("12345", replyMsgMap["blockNumber"])
	assert.Equal("0x28a62cb478a3c3d4daad84f1148ea16cd1a66f37", replyMsgMap["contractAddress"])
	assert.Equal("23456", replyMsgMap["cumulativeGasUsed"])
	assert.Equal("0xba25be62a5c55d4ad1d5520268806a8730a4de5e", replyMsgMap["from"])
	assert.Equal("345678", replyMsgMap["gasUsed"])
	assert.Equal("123", replyMsgMap["nonce"])
	assert.Equal("1", replyMsgMap["status"])
	assert.Equal("0xd7fac2bce408ed7c6ded07a32038b1f79c2b27d3", replyMsgMap["to"])
	assert.Equal("456789", replyMsgMap["transactionIndex"])
}
func TestOnDeployContractMessageGoodTxnMinedWithHex(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime:      1,
		HexValuesInReceipt: true,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnJSON

	testRPC := goodMessageRPC()
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	for inMap := false; !inMap; _, inMap = txnProcessor.inflightTxns[strings.ToLower(testFromAddr)] {
		time.Sleep(1 * time.Millisecond)
	}
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	assert.Equal(0, len(testTxnContext.errorReplies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	replyMsg := testTxnContext.replies[0]
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

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnJSON

	testRPC := goodMessageRPC()
	failStatus := hexutil.Big(*big.NewInt(0))
	testRPC.ethGetTransactionReceiptResult.Status = &failStatus
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	for inMap := false; !inMap; _, inMap = txnProcessor.inflightTxns[strings.ToLower(testFromAddr)] {
		time.Sleep(1 * time.Millisecond)
	}
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg

	txnWG.Wait()
	replyMsg := testTxnContext.replies[0]
	assert.Equal("TransactionFailure", replyMsg.ReplyHeaders().MsgType)
}

func TestOnDeployContractMessageFailedTxn(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 5000,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnJSON
	testRPC := &testRPC{
		ethSendTransactionErr: fmt.Errorf("fizzle"),
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.Equal("fizzle", testTxnContext.errorReplies[0].err.Error())
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}

func TestOnDeployContractMessageFailedToGetNonce(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	txnProcessor.conf.AlwaysManageNonce = true
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"DeployContract\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"" +
		"}"
	testRPC := &testRPC{
		ethGetTransactionCountErr: fmt.Errorf("ding"),
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.Equal("eth_getTransactionCount returned: ding", testTxnContext.errorReplies[0].err.Error())
	assert.EqualValues([]string{"eth_getTransactionCount"}, testRPC.calls)
}

func TestOnSendTransactionMessageMissingFrom(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"nonce\":\"123\"" +
		"}"
	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.NotEmpty(testTxnContext.errorReplies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("'from' must be supplied", testTxnContext.errorReplies[0].err.Error())

}

func TestOnSendTransactionMessageBadNonce(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"nonce\":\"abc\"" +
		"}"
	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.NotEmpty(testTxnContext.errorReplies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("Converting supplied 'nonce' to integer", testTxnContext.errorReplies[0].err.Error())

}

func TestOnSendTransactionMessageBadMsg(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"nonce\":\"123\"," +
		"  \"value\":\"abc\"," +
		"  \"method\":{\"name\":\"test\"}" +
		"}"
	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.NotEmpty(testTxnContext.errorReplies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("Converting supplied 'value' to big integer", testTxnContext.errorReplies[0].err.Error())

}

func TestOnSendTransactionMessageBadJSON(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "badness"
	testTxnContext.badMsgType = kldmessages.MsgTypeSendTransaction
	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.NotEmpty(testTxnContext.errorReplies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("invalid character", testTxnContext.errorReplies[0].err.Error())

}

func TestOnSendTransactionMessageTxnTimeout(t *testing.T) {
	assert := assert.New(t)

	txHash := "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b"
	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodSendTxnJSON
	testRPC := &testRPC{
		ethSendTransactionResult: txHash,
	}
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	for inMap := false; !inMap; _, inMap = txnProcessor.inflightTxns[strings.ToLower(testFromAddr)] {
		time.Sleep(1 * time.Millisecond)
	}
	txnWG := &txnProcessor.inflightTxns[strings.ToLower(testFromAddr)][0].wg
	txnWG.Wait()
	assert.Equal(1, len(testTxnContext.errorReplies))

	assert.Equal("eth_sendTransaction", testRPC.calls[0])
	assert.Equal("eth_getTransactionReceipt", testRPC.calls[1])

	assert.Regexp("Timed out waiting for transaction receipt", testTxnContext.errorReplies[0].err.Error())
	assert.Equal(txHash, testTxnContext.errorReplies[0].txHash)

}

func TestOnSendTransactionMessageFailedTxn(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodSendTxnJSON
	testRPC := &testRPC{
		ethSendTransactionErr: fmt.Errorf("pop"),
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.Equal("pop", testTxnContext.errorReplies[0].err.Error())
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}

func TestOnSendTransactionMessageFailedWithGapFillOK(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime:     1,
		SendConcurrency:   10,
		AlwaysManageNonce: true,
		AttemptGapFill:    true,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testRPC := goodMessageRPC()
	testRPC.ethSendTransactionErr = fmt.Errorf("pop")
	testRPC.ethSendTransactionErrOnce = true
	testRPC.ethSendTransactionFirstCond = sync.NewCond(&testRPC.condLock)
	testRPC.ethSendTransactionCond = sync.NewCond(&testRPC.condLock)
	txnProcessor.Init(testRPC)

	testTxnContext1 := &testTxnContext{}
	testTxnContext1.jsonMsg = goodSendTxnJSON
	testTxnContext2 := &testTxnContext{}
	testTxnContext2.jsonMsg = goodSendTxnJSON

	// Send both
	txnProcessor.OnMessage(testTxnContext1)
	txnProcessor.OnMessage(testTxnContext2)

	// Wait for both to be inflight
	from := strings.ToLower(testFromAddr)
	for len(txnProcessor.inflightTxns) == 0 || len(txnProcessor.inflightTxns[from]) < 2 {
		time.Sleep(1 * time.Millisecond)
	}

	// Let number 1 go first
	testRPC.ethSendTransactionFirstCond.L.Lock()
	testRPC.ethSendTransactionFirstReady = true
	testRPC.ethSendTransactionFirstCond.Broadcast()
	testRPC.ethSendTransactionFirstCond.L.Unlock()

	// Wait for the gap-fill
	for len(testRPC.calls) < 4 {
		time.Sleep(1 * time.Millisecond)
	}
	assert.EqualValues([]string{"eth_getTransactionCount", "eth_sendTransaction", "eth_sendTransaction", "eth_sendTransaction"}, testRPC.calls)

	// Let number 2 go second
	testRPC.ethSendTransactionCond.L.Lock()
	testRPC.ethSendTransactionReady = true
	testRPC.ethSendTransactionCond.Broadcast()
	testRPC.ethSendTransactionCond.L.Unlock()

	// Wait for the completion
	for len(testTxnContext1.errorReplies) == 0 || len(testRPC.calls) < 5 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.NotEmpty(testTxnContext1.errorReplies[0].gapFillTxHash)
	assert.True(testTxnContext1.errorReplies[0].gapFillTxSucceeded)

	assert.EqualValues([]string{"eth_getTransactionCount", "eth_sendTransaction", "eth_sendTransaction", "eth_sendTransaction", "eth_getTransactionReceipt"}, testRPC.calls)
}

func TestOnSendTransactionMessageFailedWithGapFillFail(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime:     1,
		SendConcurrency:   10,
		AlwaysManageNonce: true,
		AttemptGapFill:    true,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testRPC := goodMessageRPC()
	testRPC.ethSendTransactionErr = fmt.Errorf("pop")
	testRPC.ethSendTransactionErrOnce = false
	testRPC.ethSendTransactionFirstCond = sync.NewCond(&testRPC.condLock)
	testRPC.ethSendTransactionCond = sync.NewCond(&testRPC.condLock)
	txnProcessor.Init(testRPC)

	testTxnContext1 := &testTxnContext{}
	testTxnContext1.jsonMsg = goodSendTxnJSON
	testTxnContext2 := &testTxnContext{}
	testTxnContext2.jsonMsg = goodSendTxnJSON

	// Send both
	txnProcessor.OnMessage(testTxnContext1)
	txnProcessor.OnMessage(testTxnContext2)

	// Wait for both to be inflight
	from := strings.ToLower(testFromAddr)
	for len(txnProcessor.inflightTxns) == 0 || len(txnProcessor.inflightTxns[from]) < 2 {
		time.Sleep(1 * time.Millisecond)
	}

	// Let number 1 go first
	testRPC.ethSendTransactionFirstCond.L.Lock()
	testRPC.ethSendTransactionFirstReady = true
	testRPC.ethSendTransactionFirstCond.Broadcast()
	testRPC.ethSendTransactionFirstCond.L.Unlock()

	// Wait for the gap-fill
	for len(testRPC.calls) < 4 {
		time.Sleep(1 * time.Millisecond)
	}
	assert.EqualValues([]string{"eth_getTransactionCount", "eth_sendTransaction", "eth_sendTransaction", "eth_sendTransaction"}, testRPC.calls)

	// Let number 2 go second
	testRPC.ethSendTransactionCond.L.Lock()
	testRPC.ethSendTransactionReady = true
	testRPC.ethSendTransactionCond.Broadcast()
	testRPC.ethSendTransactionCond.L.Unlock()

	// Wait for the completion
	for len(testTxnContext1.errorReplies) == 0 || len(testRPC.calls) < 4 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.NotEmpty(testTxnContext1.errorReplies[0].gapFillTxHash)
	assert.False(testTxnContext1.errorReplies[0].gapFillTxSucceeded)

	assert.EqualValues([]string{"eth_getTransactionCount", "eth_sendTransaction", "eth_sendTransaction", "eth_sendTransaction"}, testRPC.calls)
}

func TestOnSendTransactionMessageFailedToGetNonce(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	txnProcessor.conf.AlwaysManageNonce = true
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"" +
		"}"
	testRPC := &testRPC{
		ethGetTransactionCountErr: fmt.Errorf("poof"),
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.Equal("eth_getTransactionCount returned: poof", testTxnContext.errorReplies[0].err.Error())
	assert.EqualValues([]string{"eth_getTransactionCount"}, testRPC.calls)
}

func TestOnSendTransactionMessageInflightNonce(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	txnProcessor.inflightTxns["0x83dbc8e329b38cba0fc4ed99b1ce9c2a390abdc1"] =
		[]*inflightTxn{&inflightTxn{nonce: 100}, &inflightTxn{nonce: 101}}
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"gas\":\"123\"," +
		"  \"method\":{\"name\":\"test\"}" +
		"}"
	testRPC := &testRPC{
		ethSendTransactionResult: "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)
	for len(testRPC.calls) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.Empty(testTxnContext.errorReplies)
	assert.EqualValues([]string{"eth_sendTransaction"}, testRPC.calls)
}

func TestOnSendTransactionMessageOrionNoPrivacyGroup(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		OrionPrivateAPIS: true,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"gas\":\"123\"," +
		"  \"method\":{\"name\":\"test\"}," +
		"  \"privateFrom\":\"jO6dpqnMhmnrCHqUumyK09+18diF7quq/rROGs2HFWI=\"," +
		"  \"privateFor\":[\"2QiZG7rYPzRvRsioEn6oYUff1DOvPA22EZr0+/o3RUg=\"]" +
		"}"
	testRPC := &testRPC{
		ethSendTransactionResult: "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
		privFindPrivacyGroupErr:  fmt.Errorf("pop"),
	}
	txnProcessor.Init(testRPC)
	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.NotEmpty(testTxnContext.errorReplies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("priv_findPrivacyGroup returned: pop", testTxnContext.errorReplies[0].err.Error())
}

func TestOnSendTransactionMessageOrionCannotUsePrivacyGroupIdAndPrivateFor(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		OrionPrivateAPIS: true,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"gas\":\"123\"," +
		"  \"method\":{\"name\":\"test\"}," +
		"  \"privateFrom\":\"jO6dpqnMhmnrCHqUumyK09+18diF7quq/rROGs2HFWI=\"," +
		"  \"privateFor\":[\"2QiZG7rYPzRvRsioEn6oYUff1DOvPA22EZr0+/o3RUg=\"]," +
		"  \"privacyGroupId\":\"o6fFj1vwysfp92Xt2GZlVuq14KX9HWn7oVJ+64Mfoic=\"" +
		"}"
	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.NotEmpty(testTxnContext.errorReplies)
	assert.Empty(testTxnContext.replies)
	assert.Regexp("privacyGroupId and privateFor are mutually exclusive", testTxnContext.errorReplies[0].err.Error())
}

func TestOnSendTransactionMessageOrionFailNonce(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime:    1,
		OrionPrivateAPIS: true,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	txnProcessor.inflightTxns["0x83dbc8e329b38cba0fc4ed99b1ce9c2a390abdc1"] =
		[]*inflightTxn{&inflightTxn{nonce: 100}, &inflightTxn{nonce: 101}}
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"gas\":\"123\"," +
		"  \"method\":{\"name\":\"test\"}," +
		"  \"privateFrom\":\"jO6dpqnMhmnrCHqUumyK09+18diF7quq/rROGs2HFWI=\"," +
		"  \"privateFor\":[\"2QiZG7rYPzRvRsioEn6oYUff1DOvPA22EZr0+/o3RUg=\"]" +
		"}"
	testRPC := &testRPC{
		ethGetTransactionCountErr: fmt.Errorf("pop"),
		privFindPrivacyGroupResult: []kldeth.OrionPrivacyGroup{
			kldeth.OrionPrivacyGroup{
				PrivacyGroupID: "P8SxRUussJKqZu4+nUkMJpscQeWOR3HqbAXLakatsk8=",
			},
		},
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.NotEmpty(testTxnContext.errorReplies)
	assert.Empty(testTxnContext.replies)
	assert.EqualValues([]string{"priv_findPrivacyGroup", "priv_getTransactionCount"}, testRPC.calls)
}

func TestOnSendTransactionMessageOrion(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime:    1,
		OrionPrivateAPIS: true,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	txnProcessor.inflightTxns["0x83dbc8e329b38cba0fc4ed99b1ce9c2a390abdc1"] =
		[]*inflightTxn{&inflightTxn{nonce: 100}, &inflightTxn{nonce: 101}}
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"gas\":\"123\"," +
		"  \"method\":{\"name\":\"test\"}," +
		"  \"privateFrom\":\"jO6dpqnMhmnrCHqUumyK09+18diF7quq/rROGs2HFWI=\"," +
		"  \"privateFor\":[\"2QiZG7rYPzRvRsioEn6oYUff1DOvPA22EZr0+/o3RUg=\"]" +
		"}"
	testRPC := &testRPC{
		ethSendTransactionResult: "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
		privFindPrivacyGroupResult: []kldeth.OrionPrivacyGroup{
			kldeth.OrionPrivacyGroup{
				PrivacyGroupID: "P8SxRUussJKqZu4+nUkMJpscQeWOR3HqbAXLakatsk8=",
			},
		},
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)
	for len(testRPC.calls) < 3 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.Empty(testTxnContext.errorReplies)
	assert.EqualValues([]string{"priv_findPrivacyGroup", "priv_getTransactionCount", "eea_sendTransaction"}, testRPC.calls)
}

func TestOnSendTransactionMessageOrionPrivacyGroupId(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime:    1,
		OrionPrivateAPIS: true,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	txnProcessor.inflightTxns["0x83dbc8e329b38cba0fc4ed99b1ce9c2a390abdc1"] =
		[]*inflightTxn{&inflightTxn{nonce: 100}, &inflightTxn{nonce: 101}}
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"gas\":\"123\"," +
		"  \"method\":{\"name\":\"test\"}," +
		"  \"privateFrom\":\"jO6dpqnMhmnrCHqUumyK09+18diF7quq/rROGs2HFWI=\"," +
		"  \"privacyGroupId\":\"P8SxRUussJKqZu4+nUkMJpscQeWOR3HqbAXLakatsk8=\"" +
		"}"
	testRPC := &testRPC{
		ethSendTransactionResult: "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)
	for len(testRPC.calls) < 2 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.Empty(testTxnContext.errorReplies)
	assert.EqualValues([]string{"priv_getTransactionCount", "eea_sendTransaction"}, testRPC.calls)
}

func TestCobraInitTxnProcessor(t *testing.T) {
	assert := assert.New(t)
	txconf := &TxnProcessorConf{}
	cmd := &cobra.Command{}
	CobraInitTxnProcessor(cmd, txconf)
	cmd.ParseFlags([]string{
		"-x", "10",
		"-P",
	})
	assert.Equal(10, txconf.MaxTXWaitTime)
	assert.Equal(true, txconf.AlwaysManageNonce)
}

func TestOnSendTransactionAddressBook(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.POST("/", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(500)
	})
	router.GET("/:address", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(404)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime:    1,
		OrionPrivateAPIS: true,
		AddressBookConf: AddressBookConf{
			AddressbookURLPrefix: server.URL,
		},
	}, &kldeth.RPCConf{
		RPC: kldeth.RPCConnOpts{
			URL: server.URL,
		},
	}).(*txnProcessor)
	txnProcessor.inflightTxns["0x83dbc8e329b38cba0fc4ed99b1ce9c2a390abdc1"] =
		[]*inflightTxn{&inflightTxn{nonce: 100}, &inflightTxn{nonce: 101}}
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = "{" +
		"  \"headers\":{\"type\": \"SendTransaction\"}," +
		"  \"from\":\"0x83dBC8e329b38cBA0Fc4ed99b1Ce9c2a390ABdC1\"," +
		"  \"gas\":\"123\"," +
		"  \"method\":{\"name\":\"test\"}" +
		"}"
	testRPC := &testRPC{
		ethSendTransactionResult: "0xac18e98664e160305cdb77e75e5eae32e55447e94ad8ceb0123729589ed09f8b",
	}
	txnProcessor.Init(testRPC)

	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.EqualError(testTxnContext.errorReplies[0].err, "500 Internal Server Error ")
}

func TestOnDeployContractMessageFailAddressLookup(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
		AddressBookConf: AddressBookConf{
			AddressbookURLPrefix: "   ",
		},
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodDeployTxnJSON

	testRPC := goodMessageRPC()
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.EqualError(testTxnContext.errorReplies[0].err, "Error querying Addressbook")

}

func TestOnDeployContractMessageFailHDWalletMissing(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodHDWalletDeployTxnJSON

	testRPC := goodMessageRPC()
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.EqualError(testTxnContext.errorReplies[0].err, "No HD Wallet Configuration")

}

func TestOnDeployContractMessageFailHDWalletFail(t *testing.T) {
	assert := assert.New(t)

	txnProcessor := NewTxnProcessor(&TxnProcessorConf{
		MaxTXWaitTime: 1,
		HDWalletConf:  HDWalletConf{URLTemplate: "   "},
	}, &kldeth.RPCConf{}).(*txnProcessor)
	testTxnContext := &testTxnContext{}
	testTxnContext.jsonMsg = goodHDWalletDeployTxnJSON

	testRPC := goodMessageRPC()
	txnProcessor.Init(testRPC)                          // configured in seconds for real world
	txnProcessor.maxTXWaitTime = 250 * time.Millisecond // ... but fail asap for this test

	txnProcessor.OnMessage(testTxnContext)
	for len(testTxnContext.errorReplies) == 0 {
		time.Sleep(1 * time.Millisecond)
	}

	assert.EqualError(testTxnContext.errorReplies[0].err, "HDWallet signing failed")

}
