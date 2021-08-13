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

	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/messages"
	"github.com/stretchr/testify/assert"
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
	assert.EqualError(err, "unexpected end of JSON input")
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
