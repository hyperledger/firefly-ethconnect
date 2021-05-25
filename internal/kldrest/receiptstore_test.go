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

package kldrest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"net/http/httptest"

	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethbind"
	"github.com/kaleido-io/ethconnect/internal/kldauth"
	"github.com/kaleido-io/ethconnect/internal/kldauth/kldauthtest"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
)

type mockReceiptErrs struct {
	getReceiptsErr   error
	getReceiptVal    *map[string]interface{}
	getReceiptErr    error
	addReceiptCalled bool
	addReceiptErr    error
}

func (m *mockReceiptErrs) GetReceipts(skip, limit int, ids []string, sinceEpochMS int64, from, to string) (*[]map[string]interface{}, error) {
	return nil, m.getReceiptsErr
}

func (m *mockReceiptErrs) GetReceipt(requestID string) (*map[string]interface{}, error) {
	return m.getReceiptVal, m.getReceiptErr
}

func (m *mockReceiptErrs) AddReceipt(requestID string, receipt *map[string]interface{}) error {
	m.addReceiptCalled = true
	return m.addReceiptErr
}

func newReceiptsErrTestServer(err error) (*receiptStore, *httptest.Server) {
	r := newReceiptStore(&ReceiptStoreConf{
		RetryTimeoutMS:      1,
		RetryInitialDelayMS: 1,
	}, &mockReceiptErrs{
		getReceiptErr:  fmt.Errorf("pop"),
		getReceiptsErr: fmt.Errorf("pop"),
		addReceiptErr:  fmt.Errorf("pop"),
	}, nil)
	router := &httprouter.Router{}
	r.addRoutes(router)
	return r, httptest.NewServer(router)
}

func newReceiptsTestStore() (*receiptStore, *memoryReceipts) {
	conf := &ReceiptStoreConf{
		MaxDocs:    50,
		QueryLimit: 50,
	}
	p := newMemoryReceipts(conf)
	r := newReceiptStore(conf, p, nil)
	return r, p
}

func newReceiptsTestServer() (*receiptStore, *memoryReceipts, *httptest.Server) {
	r, p := newReceiptsTestStore()
	router := &httprouter.Router{}
	r.addRoutes(router)
	return r, p, httptest.NewServer(router)
}

func TestReplyProcessorWithValidReply(t *testing.T) {
	assert := assert.New(t)

	r, p := newReceiptsTestStore()

	replyMsg := &kldmessages.TransactionReceipt{}
	replyMsg.Headers.MsgType = kldmessages.MsgTypeTransactionSuccess
	replyMsg.Headers.ID = kldutils.UUIDv4()
	replyMsg.Headers.ReqID = kldutils.UUIDv4()
	replyMsg.Headers.ReqOffset = "topic:1:2"
	txHash := ethbind.HexToHash("0x02587104e9879911bea3d5bf6ccd7e1a6cb9a03145b8a1141804cebd6aa67c5c")
	replyMsg.TransactionHash = &txHash
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	r.processReply(replyMsgBytes)

	assert.Equal(1, p.receipts.Len())
	front := *p.receipts.Front().Value.(*map[string]interface{})
	assert.Equal(replyMsg.Headers.ReqID, front["_id"])

}

func TestReplyProcessorWithContractGWSuccess(t *testing.T) {
	assert := assert.New(t)

	r, p := newReceiptsTestStore()
	r.smartContractGW = &mockContractGW{}

	replyMsg := &kldmessages.TransactionReceipt{}
	replyMsg.Headers.MsgType = kldmessages.MsgTypeTransactionSuccess
	replyMsg.Headers.ID = kldutils.UUIDv4()
	replyMsg.Headers.ReqID = kldutils.UUIDv4()
	replyMsg.Headers.ReqOffset = "topic:1:2"
	txHash := ethbind.HexToHash("0x02587104e9879911bea3d5bf6ccd7e1a6cb9a03145b8a1141804cebd6aa67c5c")
	replyMsg.TransactionHash = &txHash
	addr := ethbind.HexToAddress("0x0123456789AbcdeF0123456789abCdef0123456")
	replyMsg.ContractAddress = &addr
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	r.processReply(replyMsgBytes)

	assert.Equal(1, p.receipts.Len())
	front := *p.receipts.Front().Value.(*map[string]interface{})
	assert.Equal(replyMsg.Headers.ReqID, front["_id"])

}

func TestReplyProcessorWithContractGWFailure(t *testing.T) {
	assert := assert.New(t)

	r, p := newReceiptsTestStore()
	r.smartContractGW = &mockContractGW{
		postDeployErr: fmt.Errorf("pop"),
	}

	replyMsg := &kldmessages.TransactionReceipt{}
	replyMsg.Headers.MsgType = kldmessages.MsgTypeTransactionSuccess
	replyMsg.Headers.ID = kldutils.UUIDv4()
	replyMsg.Headers.ReqID = kldutils.UUIDv4()
	replyMsg.Headers.ReqOffset = "topic:1:2"
	txHash := ethbind.HexToHash("0x02587104e9879911bea3d5bf6ccd7e1a6cb9a03145b8a1141804cebd6aa67c5c")
	replyMsg.TransactionHash = &txHash
	addr := ethbind.HexToAddress("0x0123456789AbcdeF0123456789abCdef0123456")
	replyMsg.ContractAddress = &addr
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	r.processReply(replyMsgBytes)

	assert.Equal(1, p.receipts.Len())
	front := *p.receipts.Front().Value.(*map[string]interface{})
	assert.Equal(replyMsg.Headers.ReqID, front["_id"])

}

func TestReplyProcessorWithContractGWBadReceipt(t *testing.T) {
	r, _ := newReceiptsTestStore()
	r.smartContractGW = &mockContractGW{}

	replyMsg := map[string]interface{}{
		"headers": map[string]interface{}{
			"type":      kldmessages.MsgTypeTransactionSuccess,
			"requestId": "123",
		},
		"contractAddress": "bad address", // cannot parse as address
	}
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	r.processReply(replyMsgBytes)
}

func TestReplyProcessorWithInvalidReplySwallowsErr(t *testing.T) {
	r, _ := newReceiptsTestStore()
	r.processReply([]byte("!json"))
}

func TestReplyProcessorWithPeristenceErrorPanics(t *testing.T) {
	r := newReceiptStore(&ReceiptStoreConf{
		RetryTimeoutMS:      1,
		RetryInitialDelayMS: 1,
	}, &mockReceiptErrs{
		addReceiptErr: fmt.Errorf("pop"),
	}, nil)

	replyMsg := &kldmessages.TransactionReceipt{}
	replyMsg.Headers.MsgType = kldmessages.MsgTypeTransactionSuccess
	replyMsg.Headers.ID = kldutils.UUIDv4()
	replyMsg.Headers.ReqID = kldutils.UUIDv4()
	replyMsg.Headers.ReqOffset = "topic:1:2"
	txHash := ethbind.HexToHash("0x02587104e9879911bea3d5bf6ccd7e1a6cb9a03145b8a1141804cebd6aa67c5c")
	replyMsg.TransactionHash = &txHash
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	assert.Panics(t, func() {
		r.processReply(replyMsgBytes)
	})
}

func TestReplyProcessorWithPeristenceErrorDuplicateSwallows(t *testing.T) {
	existing := map[string]interface{}{"some": "existing"}
	mr := &mockReceiptErrs{
		addReceiptErr: fmt.Errorf("pop"),
		getReceiptErr: nil,
		getReceiptVal: &existing,
	}
	r := newReceiptStore(&ReceiptStoreConf{
		RetryTimeoutMS:      1,
		RetryInitialDelayMS: 1,
	}, mr, nil)

	replyMsg := &kldmessages.TransactionReceipt{}
	replyMsg.Headers.MsgType = kldmessages.MsgTypeTransactionSuccess
	replyMsg.Headers.ID = kldutils.UUIDv4()
	replyMsg.Headers.ReqID = kldutils.UUIDv4()
	replyMsg.Headers.ReqOffset = "topic:1:2"
	txHash := ethbind.HexToHash("0x02587104e9879911bea3d5bf6ccd7e1a6cb9a03145b8a1141804cebd6aa67c5c")
	replyMsg.TransactionHash = &txHash
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	r.processReply(replyMsgBytes)

	assert.True(t, mr.addReceiptCalled)

}

func TestReplyProcessorWithErrorReply(t *testing.T) {
	assert := assert.New(t)

	r, p := newReceiptsTestStore()

	replyMsg := &kldmessages.ErrorReply{}
	replyMsg.Headers.MsgType = kldmessages.MsgTypeError
	replyMsg.Headers.ID = kldutils.UUIDv4()
	replyMsg.Headers.ReqID = kldutils.UUIDv4()
	replyMsg.Headers.ReqOffset = "topic:1:2"
	replyMsg.OriginalMessage = "{\"badness\": true}"
	replyMsg.ErrorMessage = "pop"
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	r.processReply(replyMsgBytes)

	assert.Equal(1, p.receipts.Len())
	front := *p.receipts.Front().Value.(*map[string]interface{})
	assert.Equal(replyMsg.Headers.ReqID, front["_id"])
	assert.Equal(replyMsg.ErrorMessage, front["errorMessage"])
	assert.Equal(replyMsg.OriginalMessage, front["requestPayload"])
}

func TestReplyProcessorMissingHeaders(t *testing.T) {
	assert := assert.New(t)

	r, p := newReceiptsTestStore()

	emptyMsg := make(map[string]interface{})
	msgBytes, _ := json.Marshal(&emptyMsg)
	r.processReply(msgBytes)

	assert.Equal(0, p.receipts.Len())
}

func TestReplyProcessorMissingRequestId(t *testing.T) {
	assert := assert.New(t)

	r, p := newReceiptsTestStore()

	replyMsg := &kldmessages.ErrorReply{}
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	r.processReply(replyMsgBytes)

	assert.Equal(0, p.receipts.Len())
}

func TestReplyProcessorInsertError(t *testing.T) {
	assert := assert.New(t)

	r, p := newReceiptsTestStore()

	replyMsg := &kldmessages.ErrorReply{}
	replyMsg.Headers.ReqID = kldutils.UUIDv4()
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	r.processReply(replyMsgBytes)

	assert.Equal(1, p.receipts.Len())
}

func testGETObject(ts *httptest.Server, path string) (int, map[string]interface{}, error) {
	url := fmt.Sprintf("%s%s", ts.URL, path)
	resp, httpErr := http.Get(url)
	if httpErr != nil {
		return 0, nil, httpErr
	}
	respJSON := make(map[string]interface{})
	err := json.NewDecoder(resp.Body).Decode(&respJSON)
	return resp.StatusCode, respJSON, err
}

func testGETArray(ts *httptest.Server, path string) (int, []map[string]interface{}, error) {
	url := fmt.Sprintf("%s%s", ts.URL, path)
	resp, httpErr := http.Get(url)
	if httpErr != nil {
		return 0, nil, httpErr
	}
	respJSON := make([]map[string]interface{}, 0)
	var err error
	if resp.StatusCode == 200 {
		err = json.NewDecoder(resp.Body).Decode(&respJSON)
	}
	return resp.StatusCode, respJSON, err
}

func TestGetReplyMissing(t *testing.T) {
	assert := assert.New(t)
	_, _, ts := newReceiptsTestServer()
	defer ts.Close()

	status, respJSON, httpErr := testGETObject(ts, "/reply/ABCDEFG")
	assert.NoError(httpErr)
	assert.Equal(404, status)
	assert.Equal("Receipt not available", respJSON["error"])
}

func TestGetReplyError(t *testing.T) {
	assert := assert.New(t)
	_, ts := newReceiptsErrTestServer(fmt.Errorf("pop"))
	defer ts.Close()

	status, respJSON, httpErr := testGETObject(ts, "/reply/ABCDEFG")
	assert.NoError(httpErr)
	assert.Equal(500, status)
	assert.Equal("Error querying reply: pop", respJSON["error"])
}

func TestGetReplyOK(t *testing.T) {
	assert := assert.New(t)
	_, p, ts := newReceiptsTestServer()
	defer ts.Close()

	fakeReply1 := make(map[string]interface{})
	fakeReply1["_id"] = "ABCDEFG"
	fakeReply1["field1"] = "value1"
	p.AddReceipt("_id", &fakeReply1)
	fakeReply2 := make(map[string]interface{})
	fakeReply2["_id"] = "BCDEFG"
	fakeReply2["field1"] = "value2"
	p.AddReceipt("_id", &fakeReply2)
	status, respJSON, httpErr := testGETObject(ts, "/reply/ABCDEFG")
	assert.NoError(httpErr)
	assert.Equal(200, status)
	assert.Equal("value1", respJSON["field1"])
}

func TestGetReplyBadData(t *testing.T) {
	assert := assert.New(t)
	_, p, ts := newReceiptsTestServer()
	defer ts.Close()

	fakeReply := make(map[string]interface{})
	fakeReply["_id"] = "ABCDEFG"
	unserializable := make(map[interface{}]interface{})
	unserializable[true] = "not for json"
	fakeReply["badness"] = unserializable
	p.AddReceipt("_id", &fakeReply)
	status, respJSON, httpErr := testGETObject(ts, "/reply/ABCDEFG")
	assert.NoError(httpErr)
	assert.Equal(500, status)
	assert.Equal("Error serializing response", respJSON["error"])
}

func TestGetRepliesNoStore(t *testing.T) {
	assert := assert.New(t)
	r, _, ts := newReceiptsTestServer()
	r.persistence = nil // remove the store
	defer ts.Close()

	status, respJSON, httpErr := testGETObject(ts, "/replies")
	assert.NoError(httpErr)
	assert.Equal(405, status)
	assert.Equal("Receipt store not enabled", respJSON["error"])
}

func TestGetRepliesEmpty(t *testing.T) {
	assert := assert.New(t)
	_, _, ts := newReceiptsTestServer()
	defer ts.Close()

	status, respArr, httpErr := testGETArray(ts, "/replies")
	assert.NoError(httpErr)
	assert.Equal(200, status)
	assert.Len(respArr, 0)
}

func TestGetRepliesBadFilter(t *testing.T) {
	assert := assert.New(t)
	_, _, ts := newReceiptsTestServer()
	defer ts.Close()

	status, respJSON, httpErr := testGETArray(ts, "/replies?id=!!!!")
	assert.NoError(httpErr)
	assert.Equal(400, status)
	assert.Equal(0, len(respJSON))
}

func TestGetRepliesError(t *testing.T) {
	assert := assert.New(t)
	_, ts := newReceiptsErrTestServer(fmt.Errorf("pop"))
	defer ts.Close()

	status, respJSON, httpErr := testGETObject(ts, "/replies")
	assert.NoError(httpErr)
	assert.Equal(500, status)
	assert.Equal("Error querying replies: pop", respJSON["error"])
}

func TestGetRepliesDefaultLimit(t *testing.T) {
	assert := assert.New(t)
	_, p, ts := newReceiptsTestServer()
	defer ts.Close()

	for i := 0; i < 20; i++ {
		fakeReply := make(map[string]interface{})
		fakeReply["_id"] = fmt.Sprintf("reply%d", i)
		p.AddReceipt("_id", &fakeReply)
	}

	status, respArr, httpErr := testGETArray(ts, "/replies")
	assert.NoError(httpErr)
	assert.Equal(200, status)
	assert.Len(respArr, defaultReceiptLimit)
	for i := 0; i < defaultReceiptLimit; i++ {
		assert.Equal(fmt.Sprintf("reply%d", 20-i-1), respArr[i]["_id"])
	}
}

func TestGetRepliesCustomSkipLimit(t *testing.T) {
	assert := assert.New(t)
	_, p, ts := newReceiptsTestServer()
	defer ts.Close()

	for i := 0; i < 20; i++ {
		fakeReply := make(map[string]interface{})
		fakeReply["_id"] = fmt.Sprintf("reply%d", i)
		p.AddReceipt("_id", &fakeReply)
	}

	status, respArr, httpErr := testGETArray(ts, "/replies?skip=5&limit=20")
	assert.NoError(httpErr)
	assert.Equal(200, status)
	assert.Len(respArr, 15) // only 15 left, limit was 20
	for i := 0; i < 15; i++ {
		assert.Equal(fmt.Sprintf("reply%d", 15-i-1), respArr[i]["_id"])
	}
}

func TestGetRepliesCustomFiltersISO(t *testing.T) {
	assert := assert.New(t)
	_, p, ts := newReceiptsTestServer()
	defer ts.Close()

	for i := 0; i < 20; i++ {
		fakeReply := make(map[string]interface{})
		fakeReply["_id"] = fmt.Sprintf("reply%d", i)
		p.AddReceipt("_id", &fakeReply)
	}

	status, resObj, httpErr := testGETObject(ts, "/replies?from=abc&to=bcd&since=2019-01-01T00:00:00Z")
	assert.NoError(httpErr)
	assert.Equal(500, status)
	assert.Equal("Error querying replies: Memory receipts do not support filtering", resObj["error"])
}

func TestGetRepliesCustomFiltersTS(t *testing.T) {
	assert := assert.New(t)
	_, p, ts := newReceiptsTestServer()
	defer ts.Close()

	for i := 0; i < 20; i++ {
		fakeReply := make(map[string]interface{})
		fakeReply["_id"] = fmt.Sprintf("reply%d", i)
		p.AddReceipt("_id", &fakeReply)
	}

	status, resObj, httpErr := testGETObject(ts, "/replies?from=abc&to=bcd&since=1580435959")
	assert.NoError(httpErr)
	assert.Equal(500, status)
	assert.Equal("Error querying replies: Memory receipts do not support filtering", resObj["error"])
}

func TestGetRepliesBadSinceTS(t *testing.T) {
	assert := assert.New(t)
	_, p, ts := newReceiptsTestServer()
	defer ts.Close()

	for i := 0; i < 20; i++ {
		fakeReply := make(map[string]interface{})
		fakeReply["_id"] = fmt.Sprintf("reply%d", i)
		p.AddReceipt("_id", &fakeReply)
	}

	status, resObj, httpErr := testGETObject(ts, "/replies?from=abc&to=bcd&since=badness")
	assert.NoError(httpErr)
	assert.Equal(400, status)
	assert.Equal("since cannot be parsed as RFC3339 or millisecond timestamp", resObj["error"])
}

func TestGetRepliesInvalidLimit(t *testing.T) {
	assert := assert.New(t)
	_, _, ts := newReceiptsTestServer()
	defer ts.Close()

	status, respJSON, httpErr := testGETObject(ts, "/replies?limit=bad&skip=10")
	assert.NoError(httpErr)
	assert.Equal(400, status)
	assert.Equal("Invalid 'limit' query parameter", respJSON["error"])
}

func TestGetRepliesInvalidSkip(t *testing.T) {
	assert := assert.New(t)
	_, _, ts := newReceiptsTestServer()
	defer ts.Close()

	status, respJSON, httpErr := testGETObject(ts, "/replies?limit=10&skip=bad")
	assert.NoError(httpErr)
	assert.Equal(400, status)
	assert.Equal("Invalid 'skip' query parameter", respJSON["error"])
}

func TestGetRepliesExcessiveLimit(t *testing.T) {
	assert := assert.New(t)
	_, _, ts := newReceiptsTestServer()
	defer ts.Close()

	status, respJSON, httpErr := testGETObject(ts, "/replies?limit=1000")
	assert.NoError(httpErr)
	assert.Equal(400, status)
	assert.Equal("Maximum limit is 50", respJSON["error"])
}

func TestGetRepliesUnauthorized(t *testing.T) {
	kldauth.RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	assert := assert.New(t)
	_, _, ts := newReceiptsTestServer()
	defer ts.Close()

	status, respJSON, httpErr := testGETObject(ts, "/replies?limit=1000")
	assert.NoError(httpErr)
	assert.Equal(401, status)
	assert.Equal("Unauthorized", respJSON["error"])

	kldauth.RegisterSecurityModule(nil)
}

func TestGetReplyUnauthorized(t *testing.T) {
	kldauth.RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	assert := assert.New(t)
	_, _, ts := newReceiptsTestServer()
	defer ts.Close()

	status, respJSON, httpErr := testGETObject(ts, "/reply/12345")
	assert.NoError(httpErr)
	assert.Equal(401, status)
	assert.Equal("Unauthorized", respJSON["error"])

	kldauth.RegisterSecurityModule(nil)
}
