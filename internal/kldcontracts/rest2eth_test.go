// Copyright 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldcontracts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/kaleido-io/ethconnect/internal/kldevents"

	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type mockREST2EthDispatcher struct {
	asyncDispatchMsg           map[string]interface{}
	asyncDispatchAck           bool
	asyncDispatchReply         *kldmessages.AsyncSentMsg
	asyncDispatchError         error
	sendTransactionMsg         *kldmessages.SendTransaction
	sendTransactionSyncReceipt *kldmessages.TransactionReceipt
	sendTransactionSyncError   error
	deployContractMsg          *kldmessages.DeployContract
	deployContractSyncReceipt  *kldmessages.TransactionReceipt
	deployContractSyncError    error
}

func (m *mockREST2EthDispatcher) DispatchMsgAsync(msg map[string]interface{}, ack bool) (*kldmessages.AsyncSentMsg, error) {
	m.asyncDispatchMsg = msg
	m.asyncDispatchAck = ack
	return m.asyncDispatchReply, m.asyncDispatchError
}

func (m *mockREST2EthDispatcher) DispatchSendTransactionSync(msg *kldmessages.SendTransaction, replyProcessor rest2EthReplyProcessor) {
	m.sendTransactionMsg = msg
	if m.sendTransactionSyncError != nil {
		replyProcessor.ReplyWithError(m.sendTransactionSyncError)
	} else {
		replyProcessor.ReplyWithReceipt(m.sendTransactionSyncReceipt)
	}
}

func (m *mockREST2EthDispatcher) DispatchDeployContractSync(msg *kldmessages.DeployContract, replyProcessor rest2EthReplyProcessor) {
	m.deployContractMsg = msg
	if m.deployContractSyncError != nil {
		replyProcessor.ReplyWithError(m.deployContractSyncError)
	} else {
		replyProcessor.ReplyWithReceipt(m.deployContractSyncReceipt)
	}
}

type mockABILoader struct {
	loadABIError           error
	abi                    *kldbind.ABI
	deployMsg              *kldmessages.DeployContract
	registeredContractAddr string
	resolveContractErr     error
	nameAvailableError     error
}

func (m *mockABILoader) loadABIForInstance(addrHexNo0x string) (*kldbind.ABI, error) {
	return m.abi, m.loadABIError
}

func (m *mockABILoader) resolveContractAddr(registeredName string) (string, error) {
	return m.registeredContractAddr, m.resolveContractErr
}

func (m *mockABILoader) loadDeployMsgForFactory(addrHexNo0x string) (*kldmessages.DeployContract, error) {
	return m.deployMsg, m.loadABIError
}

func (m *mockABILoader) checkNameAvailable(name string) error {
	return m.nameAvailableError
}

func (m *mockABILoader) PreDeploy(msg *kldmessages.DeployContract) error      { return nil }
func (m *mockABILoader) PostDeploy(msg *kldmessages.TransactionReceipt) error { return nil }
func (m *mockABILoader) AddRoutes(router *httprouter.Router)                  { return }

type mockRPC struct {
	capturedMethod string
	mockError      error
	result         interface{}
}

func (m *mockRPC) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	m.capturedMethod = method
	v := reflect.ValueOf(result)
	v.Elem().Set(reflect.ValueOf(m.result))
	return m.mockError
}

type mockSubMgr struct {
	err       error
	sub       *kldevents.SubscriptionInfo
	stream    *kldevents.StreamInfo
	subs      []*kldevents.SubscriptionInfo
	streams   []*kldevents.StreamInfo
	suspended bool
	resumed   bool
}

func (m *mockSubMgr) Init() error { return m.err }
func (m *mockSubMgr) AddStream(spec *kldevents.StreamInfo) (*kldevents.StreamInfo, error) {
	return spec, m.err
}
func (m *mockSubMgr) Streams() []*kldevents.StreamInfo                    { return m.streams }
func (m *mockSubMgr) StreamByID(id string) (*kldevents.StreamInfo, error) { return m.stream, m.err }
func (m *mockSubMgr) SuspendStream(id string) error                       { m.suspended = true; return m.err }
func (m *mockSubMgr) ResumeStream(id string) error                        { m.resumed = true; return m.err }
func (m *mockSubMgr) DeleteStream(id string) error                        { return m.err }
func (m *mockSubMgr) AddSubscription(addr *kldbind.Address, event *kldbind.ABIEvent, streamID string) (*kldevents.SubscriptionInfo, error) {
	return m.sub, m.err
}
func (m *mockSubMgr) Subscriptions() []*kldevents.SubscriptionInfo { return m.subs }
func (m *mockSubMgr) SubscriptionByID(id string) (*kldevents.SubscriptionInfo, error) {
	return m.sub, m.err
}
func (m *mockSubMgr) DeleteSubscription(id string) error { return m.err }
func (m *mockSubMgr) Close()                             {}

func newTestREST2Eth(dispatcher *mockREST2EthDispatcher) (*rest2eth, *mockRPC, *httprouter.Router) {
	mockRPC := &mockRPC{}
	compiled, _ := kldeth.CompileContract(simpleEventsSource(), "SimpleEvents", "")
	a := &kldbind.ABI{ABI: *compiled.ABI}
	deployMsg := &kldmessages.DeployContract{ABI: a}
	abiLoader := &mockABILoader{
		abi:       a,
		deployMsg: deployMsg,
	}
	r := newREST2eth(abiLoader, mockRPC, nil, dispatcher, dispatcher)
	router := &httprouter.Router{}
	r.addRoutes(router)

	return r, mockRPC, router
}

func newTestREST2EthAndMsg(dispatcher *mockREST2EthDispatcher, from, to string, bodyMap map[string]interface{}) (*rest2eth, *mockRPC, *httprouter.Router, *httptest.ResponseRecorder, *http.Request) {
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set", bytes.NewReader(body))
	req.Header.Add("x-kaleido-from", from)
	res := httptest.NewRecorder()

	r, mockRPC, router := newTestREST2Eth(dispatcher)

	return r, mockRPC, router, res, req
}

func TestSendTransactionAsyncSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &kldmessages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	req.Header.Set("X-Kaleido-PrivateFrom", "0xdC416B907857Fa8c0e0d55ec21766Ee3546D5f90")
	req.Header.Set("X-Kaleido-PrivateFor", "0xE7E32f0d5A2D55B2aD27E0C2d663807F28f7c745,0xB92F8CebA52fFb5F08f870bd355B1d32f0fd9f7C")
	router.ServeHTTP(res, req)

	assert.Equal(202, res.Result().StatusCode)
	reply := kldmessages.AsyncSentMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal(true, reply.Sent)
	assert.Equal("request1", reply.Request)

	assert.Equal(true, dispatcher.asyncDispatchAck)
	assert.Equal(from, dispatcher.asyncDispatchMsg["from"])
	assert.Equal(to, dispatcher.asyncDispatchMsg["to"])
	assert.Equal("0xdC416B907857Fa8c0e0d55ec21766Ee3546D5f90", dispatcher.asyncDispatchMsg["privateFrom"])
	assert.Equal("0xE7E32f0d5A2D55B2aD27E0C2d663807F28f7c745", dispatcher.asyncDispatchMsg["privateFor"].([]interface{})[0])
	assert.Equal("0xB92F8CebA52fFb5F08f870bd355B1d32f0fd9f7C", dispatcher.asyncDispatchMsg["privateFor"].([]interface{})[1])
}

func TestDeployContractAsyncSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &kldmessages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1?kld-privateFrom=0xdC416B907857Fa8c0e0d55ec21766Ee3546D5f90&kld-privateFor=0xE7E32f0d5A2D55B2aD27E0C2d663807F28f7c745&kld-privateFor=0xB92F8CebA52fFb5F08f870bd355B1d32f0fd9f7C", bytes.NewReader(body))
	req.Header.Add("x-kaleido-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(202, res.Result().StatusCode)
	reply := kldmessages.AsyncSentMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal(true, reply.Sent)
	assert.Equal("request1", reply.Request)

	assert.Equal(true, dispatcher.asyncDispatchAck)
	assert.Equal(from, dispatcher.asyncDispatchMsg["from"])
	assert.Equal("0xdC416B907857Fa8c0e0d55ec21766Ee3546D5f90", dispatcher.asyncDispatchMsg["privateFrom"])
	assert.Equal("0xE7E32f0d5A2D55B2aD27E0C2d663807F28f7c745", dispatcher.asyncDispatchMsg["privateFor"].([]interface{})[0])
	assert.Equal("0xB92F8CebA52fFb5F08f870bd355B1d32f0fd9f7C", dispatcher.asyncDispatchMsg["privateFor"].([]interface{})[1])
}

func TestDeployContractAsyncDuplicate(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &kldmessages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	r, _, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	abiLoader := r.gw.(*mockABILoader)
	abiLoader.nameAvailableError = fmt.Errorf("spent already")
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1?kld-privateFrom=0xdC416B907857Fa8c0e0d55ec21766Ee3546D5f90&kld-privateFor=0xE7E32f0d5A2D55B2aD27E0C2d663807F28f7c745&kld-privateFor=0xB92F8CebA52fFb5F08f870bd355B1d32f0fd9f7C", bytes.NewReader(body))
	req.Header.Add("x-kaleido-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(409, res.Result().StatusCode)
	var resBody map[string]interface{}
	json.NewDecoder(res.Body).Decode(&resBody)
	assert.Equal("spent already", resBody["error"])
}

func TestSendTransactionSyncSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	receipt := &kldmessages.TransactionReceipt{}
	dispatcher := &mockREST2EthDispatcher{
		sendTransactionSyncReceipt: receipt,
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?kld-sync&kld-ethvalue=1234", bytes.NewReader(body))
	req.Header.Add("x-kaleido-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(json.Number("1234"), dispatcher.sendTransactionMsg.Value)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)
}

func TestSendTransactionSyncViaABISuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	abi := "69a8898a-b6ef-4092-43bf-6cbffac56939"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	receipt := &kldmessages.TransactionReceipt{}
	dispatcher := &mockREST2EthDispatcher{
		sendTransactionSyncReceipt: receipt,
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/"+abi+"/"+to+"/set?kld-sync&kld-ethvalue=1234", bytes.NewReader(body))
	req.Header.Add("x-kaleido-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(json.Number("1234"), dispatcher.sendTransactionMsg.Value)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)
}

func TestDeployContractSyncSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	receipt := &kldmessages.TransactionReceipt{}
	dispatcher := &mockREST2EthDispatcher{
		deployContractSyncReceipt: receipt,
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1?kld-sync", bytes.NewReader(body))
	req.Header.Add("x-kaleido-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(from, dispatcher.deployContractMsg.From)
}

func TestSendTransactionSyncFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		sendTransactionSyncError: fmt.Errorf("pop"),
	}
	_, _, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	req.Header.Set("x-kaleido-sync", "true")
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)
}

func TestSendTransactionAsyncFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchError: fmt.Errorf("pop"),
	}
	_, _, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)
}

func TestDeployContractAsyncFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchError: fmt.Errorf("pop"),
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1", bytes.NewReader(body))
	req.Header.Add("x-kaleido-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)
}

func TestSendTransactionAsyncBadMethod(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchError: fmt.Errorf("pop"),
	}
	_, _, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)
}
func TestSendTransactionBadContract(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "badness"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchError: fmt.Errorf("pop"),
	}
	_, _, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)
}

func TestSendTransactionUnknownRegisteredName(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "random"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{}
	r, _, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	abiLoader := r.gw.(*mockABILoader)
	abiLoader.resolveContractErr = fmt.Errorf("unregistered")
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("unregistered", reply.Message)
}

func TestSendTransactionMissingContract(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	from := ""
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchError: fmt.Errorf("pop"),
	}
	_, _, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Please specify a valid address in the 'kld-from' query string parameter or x-kaleido-from HTTP header", reply.Message)
}

func TestSendTransactionBadFrom(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "66C5FE653E7A9EBB628A6D40F0452D1E358BAEE8"
	from := "badness"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchError: fmt.Errorf("pop"),
	}
	_, _, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("From Address must be a 40 character hex string (0x prefix is optional)", reply.Message)
}

func TestSendTransactionInvalidContract(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["x"] = 12345
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &kldmessages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	r, _, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	abiLoader := r.gw.(*mockABILoader)
	abiLoader.abi = nil
	abiLoader.loadABIError = fmt.Errorf("pop")
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)
}

func TestDeployContractInvalidABI(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &kldmessages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	r, _, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1?kld-sync", bytes.NewReader(body))
	req.Header.Add("x-kaleido-from", from)
	abiLoader := r.gw.(*mockABILoader)
	abiLoader.abi = nil
	abiLoader.loadABIError = fmt.Errorf("pop")
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)
}
func TestSendTransactionInvalidMethod(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &kldmessages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/shazaam", bytes.NewReader([]byte("{}")))
	req.Header.Set("x-kaleido-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Method or Event 'shazaam' is not declared in the ABI of contract '567a417717cb6c59ddc1035705f02c0fd1ab1872'", reply.Message)
}

func TestSendTransactionParamInQuery(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &kldmessages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?i=999&s=msg&kld-ethvalue=12345", bytes.NewReader([]byte("{}")))
	req.Header.Set("x-kaleido-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(202, res.Result().StatusCode)
}

func TestSendTransactionMissingParam(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &kldmessages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Parameter 'i' of method 'set' was not specified in body or query parameters", reply.Message)
}

func TestSendTransactionBadBody(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &kldmessages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?x=999", bytes.NewReader([]byte(":not json or yaml")))
	req.Header.Set("x-kaleido-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Regexp("Unable to parse as YAML or JSON", reply.Message)
}

func TestCallMethodSuccess(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}
	_, mockRPC, router, res, _ := newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	req := httptest.NewRequest("GET", "/contracts/"+to+"/get", bytes.NewReader([]byte{}))
	mockRPC.result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("eth_call", mockRPC.capturedMethod)
	var reply map[string]interface{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	log.Infof("Reply: %+v", reply)
	assert.Nil(reply["error"])
	assert.Equal("123456", reply["i"])
	assert.Equal("testing", reply["s"])
}

func TestCallReadOnlyMethodViaPOSTSuccess(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}
	_, mockRPC, router, res, _ := newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	req := httptest.NewRequest("POST", "/contracts/"+to+"/get", bytes.NewReader([]byte{}))
	mockRPC.result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("eth_call", mockRPC.capturedMethod)
	var reply map[string]interface{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	log.Infof("Reply: %+v", reply)
	assert.Nil(reply["error"])
	assert.Equal("123456", reply["i"])
	assert.Equal("testing", reply["s"])
}

func TestCallMethodFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}
	_, mockRPC, router, res, _ := newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	mockRPC.result = ""
	mockRPC.mockError = fmt.Errorf("pop")
	req := httptest.NewRequest("GET", "/contracts/"+to+"/get", bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Regexp("Call failed: pop", reply.Message)
}

func TestCallMethodViaABIBadAddress(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "badness"
	dispatcher := &mockREST2EthDispatcher{}
	_, _, router, res, _ := newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	req := httptest.NewRequest("GET", "/abis/ABI1/badaddress/get", bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("To Address must be a 40 character hex string (0x prefix is optional)", reply.Message)
}

func TestSubscribeNoAddressNoSubMgr(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	_, _, router := newTestREST2Eth(dispatcher)
	bodyBytes, _ := json.Marshal(&map[string]string{
		"stream": "stream1",
	})
	req := httptest.NewRequest("POST", "/abis/ABI1/Changed/SuBsCrIbE", bytes.NewReader(bodyBytes))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(405, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Event support is not configured on this gateway", reply.Message)
}

func TestSubscribeNoAddressUnknownEvent(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	_, _, router := newTestREST2Eth(dispatcher)
	bodyBytes, _ := json.Marshal(&map[string]string{
		"stream": "stream1",
	})
	req := httptest.NewRequest("POST", "/abis/ABI1/lobster/subscribe", bytes.NewReader(bodyBytes))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Event 'subscribe' is not declared in the ABI", reply.Message)
}

func TestSubscribeNoAddressMissingStream(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, _, router := newTestREST2Eth(dispatcher)
	r.subMgr = &mockSubMgr{}
	bodyBytes, _ := json.Marshal(&map[string]string{})
	req := httptest.NewRequest("POST", "/abis/ABI1/Changed/subscribe", bytes.NewReader(bodyBytes))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Must supply a 'stream' parameter in the body or query", reply.Message)
}

func TestSubscribeNoAddressSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, _, router := newTestREST2Eth(dispatcher)
	r.subMgr = &mockSubMgr{
		sub: &kldevents.SubscriptionInfo{ID: "sub1"},
	}
	bodyBytes, _ := json.Marshal(&map[string]string{
		"stream": "stream1",
	})
	req := httptest.NewRequest("POST", "/abis/ABI1/Changed/subscribe", bytes.NewReader(bodyBytes))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	reply := kldevents.SubscriptionInfo{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("sub1", reply.ID)
}

func TestSubscribeWithAddressSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, _, router := newTestREST2Eth(dispatcher)
	r.subMgr = &mockSubMgr{
		sub: &kldevents.SubscriptionInfo{ID: "sub1"},
	}
	bodyBytes, _ := json.Marshal(&map[string]string{
		"stream": "stream1",
	})
	req := httptest.NewRequest("POST", "/contracts/0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8/Changed/subscribe", bytes.NewReader(bodyBytes))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	reply := kldevents.SubscriptionInfo{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("sub1", reply.ID)
}

func TestSubscribeWithAddressBadAddress(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, _, router := newTestREST2Eth(dispatcher)
	abiLoader := r.gw.(*mockABILoader)
	abiLoader.resolveContractErr = fmt.Errorf("unregistered")
	r.subMgr = &mockSubMgr{
		sub: &kldevents.SubscriptionInfo{ID: "sub1"},
	}
	bodyBytes, _ := json.Marshal(&map[string]string{
		"stream": "stream1",
	})
	req := httptest.NewRequest("POST", "/contracts/badness/Changed/subscribe", bytes.NewReader(bodyBytes))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("unregistered", reply.Message)
}

func TestSubscribeWithAddressSubmgrFailure(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, _, router := newTestREST2Eth(dispatcher)
	r.subMgr = &mockSubMgr{
		err: fmt.Errorf("pop"),
	}
	bodyBytes, _ := json.Marshal(&map[string]string{
		"stream": "stream1",
	})
	req := httptest.NewRequest("POST", "/contracts/0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8/Changed/subscribe", bytes.NewReader(bodyBytes))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)
}
