// Copyright 2018, 2021 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hyperledger/firefly-ethconnect/internal/messages"
	"github.com/hyperledger/firefly-ethconnect/mocks/ethmocks"
	"github.com/hyperledger/firefly-ethconnect/mocks/ffcmocks"
	"github.com/julienschmidt/httprouter"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type popReader struct{}

func (r *popReader) Read(b []byte) (n int, err error) {
	return 0, fmt.Errorf("pop")
}

type mockContractGW struct {
	preDeployErr  error
	postDeployErr error
	testValue     interface{}
	replyCallback func(message interface{})
}

func (m *mockContractGW) PreDeploy(*messages.DeployContract) error { return m.preDeployErr }

func (m *mockContractGW) PostDeploy(*messages.TransactionReceipt) error { return m.postDeployErr }

func (m *mockContractGW) AddRoutes(*httprouter.Router) {}

func (m *mockContractGW) SendReply(message interface{}) {
	if m.replyCallback != nil {
		m.replyCallback(message)
	}
}

func (m *mockContractGW) Shutdown() {}

type mockHandler struct{}

func (*mockHandler) sendWebhookMsg(ctx context.Context, key, msgID string, msg map[string]interface{}, ack bool) (msgAck string, statusCode int, err error) {
	return "", 200, nil
}

func (*mockHandler) run() error {
	return nil
}

func (*mockHandler) isInitialized() bool {
	return true
}

func TestWebhookHandlerBadRequest(t *testing.T) {
	assert := assert.New(t)

	badReq, _ := http.NewRequest("POST", "/any", &popReader{})
	w := &webhooks{}
	rec := httptest.NewRecorder()
	w.webhookHandler(rec, badReq, false)
	assert.Equal(400, rec.Result().StatusCode)
}

func TestWebhookHandlerContractGWSuccess(t *testing.T) {
	assert := assert.New(t)

	deployMsg := messages.DeployContract{
		TransactionCommon: messages.TransactionCommon{
			RequestCommon: messages.RequestCommon{
				Headers: messages.RequestHeaders{
					CommonHeaders: messages.CommonHeaders{
						MsgType: messages.MsgTypeDeployContract,
					},
				},
			},
		},
	}
	deployMsgBytes, _ := json.Marshal(&deployMsg)
	req, _ := http.NewRequest("POST", "/any", bytes.NewReader(deployMsgBytes))
	w := &webhooks{
		smartContractGW: &mockContractGW{},
		handler:         &mockHandler{},
	}
	rec := httptest.NewRecorder()
	w.webhookHandler(rec, req, false)
	assert.Equal(200, rec.Result().StatusCode)
}

func TestWebhookHandlerContractGWImmeidateReceiptSuccess(t *testing.T) {
	assert := assert.New(t)

	deployMsg := messages.DeployContract{
		TransactionCommon: messages.TransactionCommon{
			RequestCommon: messages.RequestCommon{
				Headers: messages.RequestHeaders{
					CommonHeaders: messages.CommonHeaders{
						MsgType: messages.MsgTypeDeployContract,
					},
				},
			},
			AckType: "receipt",
		},
	}
	deployMsgBytes, _ := json.Marshal(&deployMsg)
	req, _ := http.NewRequest("POST", "/any", bytes.NewReader(deployMsgBytes))
	r := newMemoryReceipts(&ReceiptStoreConf{})
	rs := newReceiptStore(&ReceiptStoreConf{}, r, nil)
	w := &webhooks{
		smartContractGW: &mockContractGW{},
		handler:         &mockHandler{},
		receipts:        rs,
	}
	rec := httptest.NewRecorder()
	w.webhookHandler(rec, req, false)
	assert.Equal(200, rec.Result().StatusCode)

	var responseBody map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&responseBody)
	assert.NoError(err)

	id := responseBody["id"].(string)
	assert.NotEmpty(id)

	receipt, err := r.GetReceipt(id)
	assert.NoError(err)
	assert.NotNil(receipt)
	assert.Equal(id, (*receipt)["_id"].(string))
	assert.True((*receipt)["pending"].(bool))

}

func TestWebhookHandlerContractGWFail(t *testing.T) {
	assert := assert.New(t)

	deployMsg := messages.DeployContract{
		TransactionCommon: messages.TransactionCommon{
			RequestCommon: messages.RequestCommon{
				Headers: messages.RequestHeaders{
					CommonHeaders: messages.CommonHeaders{
						MsgType: messages.MsgTypeDeployContract,
					},
				},
			},
		},
	}
	deployMsgBytes, _ := json.Marshal(&deployMsg)
	req, _ := http.NewRequest("POST", "/any", bytes.NewReader(deployMsgBytes))
	w := &webhooks{
		smartContractGW: &mockContractGW{
			preDeployErr: fmt.Errorf("pop"),
		},
		handler: &mockHandler{},
	}
	rec := httptest.NewRecorder()
	w.webhookHandler(rec, req, false)
	assert.Equal(500, rec.Result().StatusCode)
}

func TestContractGWHandlerUnmarshalFail(t *testing.T) {
	assert := assert.New(t)

	w := &webhooks{
		smartContractGW: &mockContractGW{
			preDeployErr: fmt.Errorf("pop"),
		},
		handler: &mockHandler{},
	}
	_, err := w.contractGWHandler(map[string]interface{}{
		"bad json": map[bool]bool{true: false},
	})
	assert.Regexp("unexpected end of JSON input", err)
}

func TestWebhookHandlerTransaction(t *testing.T) {
	assert := assert.New(t)

	transactionMsg := messages.SendTransaction{
		TransactionCommon: messages.TransactionCommon{
			RequestCommon: messages.RequestCommon{
				Headers: messages.RequestHeaders{
					CommonHeaders: messages.CommonHeaders{
						MsgType: messages.MsgTypeDeployContract,
					},
				},
			},
		},
	}
	transactionMsgBytes, _ := json.Marshal(&transactionMsg)
	req, _ := http.NewRequest("POST", "/any", bytes.NewReader(transactionMsgBytes))
	w := &webhooks{
		smartContractGW: &mockContractGW{},
		handler:         &mockHandler{},
	}
	rec := httptest.NewRecorder()
	w.webhookHandler(rec, req, false)
	res := rec.Result()
	assert.Equal(200, res.StatusCode)

	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	assert.NoError(err)

	var asyncResponse messages.AsyncSentMsg
	err = json.Unmarshal(data, &asyncResponse)
	assert.NoError(err)
	assert.Regexp(regexp.MustCompile(`\w{8}-\w{4}-\w{4}-\w{4}-\w{12}`), asyncResponse.Request)
}

func TestWebhookHandlerFFCAPI(t *testing.T) {
	assert := assert.New(t)

	req, _ := http.NewRequest("POST", "/any", bytes.NewReader([]byte(`{
		"ffcapi": {
			"version": "v1.0.0",
			"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
			"type": "get_block_info_by_number"
		},
		"blockNumber": "12345"
	}`)))
	ffcMocks := &ffcmocks.FFCServer{}
	ffcMocks.On("ServeFFCAPI", mock.Anything, mock.Anything, mock.Anything).Return().Run(func(args mock.Arguments) {
		w := args[2].(http.ResponseWriter)
		w.Write([]byte(`{"ok": true}`))
	})

	w := &webhooks{
		smartContractGW: &mockContractGW{},
		handler:         &mockHandler{},
		ffc:             ffcMocks,
	}
	rec := httptest.NewRecorder()
	w.webhookHandler(rec, req, false)
	res := rec.Result()
	assert.Equal(200, res.StatusCode)

	defer res.Body.Close()
	var resMap map[string]interface{}
	json.NewDecoder(res.Body).Decode(&resMap)
	assert.True(resMap["ok"].(bool))

}

func TestWebhookHandlerTransactionWithID(t *testing.T) {
	assert := assert.New(t)

	transactionMsg := messages.SendTransaction{
		TransactionCommon: messages.TransactionCommon{
			RequestCommon: messages.RequestCommon{
				Headers: messages.RequestHeaders{
					CommonHeaders: messages.CommonHeaders{
						MsgType: messages.MsgTypeDeployContract,
						ID:      "test-id",
					},
				},
			},
		},
	}
	transactionMsgBytes, _ := json.Marshal(&transactionMsg)
	req, _ := http.NewRequest("POST", "/any", bytes.NewReader(transactionMsgBytes))
	w := &webhooks{
		smartContractGW: &mockContractGW{},
		handler:         &mockHandler{},
	}
	rec := httptest.NewRecorder()
	w.webhookHandler(rec, req, false)
	res := rec.Result()
	assert.Equal(200, res.StatusCode)

	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	assert.NoError(err)

	var asyncResponse messages.AsyncSentMsg
	err = json.Unmarshal(data, &asyncResponse)
	assert.NoError(err)
	assert.Equal("test-id", asyncResponse.Request)
}

func TestWebhookHandlerQuery(t *testing.T) {
	assert := assert.New(t)

	queryMsg := messages.QueryTransaction{
		SendTransaction: messages.SendTransaction{
			TransactionCommon: messages.TransactionCommon{
				RequestCommon: messages.RequestCommon{
					Headers: messages.RequestHeaders{
						CommonHeaders: messages.CommonHeaders{
							MsgType: messages.MsgTypeQuery,
						},
					},
				},
			},
			To: "0x6287111c39df2ff2aaa367f0b062f2dd86e3bcaa",
			Method: &ethbinding.ABIElementMarshaling{
				Name: "get",
			},
		},
	}
	queryMsgBytes, _ := json.Marshal(&queryMsg)
	req, _ := http.NewRequest("POST", "/any", bytes.NewReader(queryMsgBytes))
	mockRPC := &ethmocks.RPCClient{}
	mockRPC.On("CallContext", mock.Anything, mock.Anything, "eth_call", mock.Anything, "latest").Return(nil)
	w := &webhooks{
		rpcClient: mockRPC,
	}
	rec := httptest.NewRecorder()
	w.webhookHandler(rec, req, false)
	assert.Equal(200, rec.Result().StatusCode)
}

func TestWebhookHandlerQueryBadPayload(t *testing.T) {
	assert := assert.New(t)

	badMsg := map[string]interface{}{
		"headers": map[string]interface{}{
			"type": "Query",
		},
		"blockNumber": []string{"Wrong!"}}
	badMsgBytes, _ := json.Marshal(&badMsg)
	req, _ := http.NewRequest("POST", "/any", bytes.NewReader(badMsgBytes))
	w := &webhooks{}
	rec := httptest.NewRecorder()
	w.webhookHandler(rec, req, false)
	assert.Equal(400, rec.Result().StatusCode)
}

func TestWebhookHandlerQueryBadBlockNumber(t *testing.T) {
	assert := assert.New(t)

	badMsg := map[string]interface{}{
		"headers": map[string]interface{}{
			"type": "Query",
		},
		"blockNumber": "!Badnumber",
	}
	badMsgBytes, _ := json.Marshal(&badMsg)
	req, _ := http.NewRequest("POST", "/any", bytes.NewReader(badMsgBytes))
	w := &webhooks{}
	rec := httptest.NewRecorder()
	w.webhookHandler(rec, req, false)
	assert.Equal(400, rec.Result().StatusCode)
}

func TestWebhookHandlerQueryFail(t *testing.T) {
	assert := assert.New(t)

	queryMsg := messages.QueryTransaction{
		SendTransaction: messages.SendTransaction{
			TransactionCommon: messages.TransactionCommon{
				RequestCommon: messages.RequestCommon{
					Headers: messages.RequestHeaders{
						CommonHeaders: messages.CommonHeaders{
							MsgType: messages.MsgTypeQuery,
						},
					},
				},
			},
			To: "0x6287111c39df2ff2aaa367f0b062f2dd86e3bcaa",
			Method: &ethbinding.ABIElementMarshaling{
				Name: "get",
			},
		},
	}
	queryMsgBytes, _ := json.Marshal(&queryMsg)
	req, _ := http.NewRequest("POST", "/any", bytes.NewReader(queryMsgBytes))
	mockRPC := &ethmocks.RPCClient{}
	mockRPC.On("CallContext", mock.Anything, mock.Anything, "eth_call", mock.Anything, "latest").Return(fmt.Errorf("pop"))
	w := &webhooks{
		rpcClient: mockRPC,
	}
	rec := httptest.NewRecorder()
	w.webhookHandler(rec, req, false)
	assert.Equal(500, rec.Result().StatusCode)
}
