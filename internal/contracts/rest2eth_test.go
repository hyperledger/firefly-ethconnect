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

package contracts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperledger-labs/firefly-ethconnect/internal/auth"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/auth/authtest"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/eth"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/ethbind"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/events"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/messages"
	"github.com/julienschmidt/httprouter"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type mockREST2EthDispatcher struct {
	asyncDispatchMsg           map[string]interface{}
	asyncDispatchAck           bool
	asyncDispatchReply         *messages.AsyncSentMsg
	asyncDispatchError         error
	sendTransactionMsg         *messages.SendTransaction
	sendTransactionSyncReceipt *messages.TransactionReceipt
	sendTransactionSyncError   error
	deployContractMsg          *messages.DeployContract
	deployContractSyncReceipt  *messages.TransactionReceipt
	deployContractSyncError    error
}

func (m *mockREST2EthDispatcher) DispatchMsgAsync(ctx context.Context, msg map[string]interface{}, ack bool) (*messages.AsyncSentMsg, error) {
	m.asyncDispatchMsg = msg
	m.asyncDispatchAck = ack
	return m.asyncDispatchReply, m.asyncDispatchError
}

func (m *mockREST2EthDispatcher) DispatchSendTransactionSync(ctx context.Context, msg *messages.SendTransaction, replyProcessor rest2EthReplyProcessor) {
	m.sendTransactionMsg = msg
	if m.sendTransactionSyncError != nil {
		replyProcessor.ReplyWithError(m.sendTransactionSyncError)
	} else {
		replyProcessor.ReplyWithReceipt(m.sendTransactionSyncReceipt)
	}
}

func (m *mockREST2EthDispatcher) DispatchDeployContractSync(ctx context.Context, msg *messages.DeployContract, replyProcessor rest2EthReplyProcessor) {
	m.deployContractMsg = msg
	if m.deployContractSyncError != nil {
		replyProcessor.ReplyWithError(m.deployContractSyncError)
	} else {
		replyProcessor.ReplyWithReceipt(m.deployContractSyncReceipt)
	}
}

type mockABILoader struct {
	loadABIError           error
	deployMsg              *messages.DeployContract
	abiInfo                *abiInfo
	contractInfo           *contractInfo
	registeredContractAddr string
	resolveContractErr     error
	nameAvailableError     error
	capturedAddr           string
	postDeployError        error
}

func (m *mockABILoader) SendReply(message interface{}) {

}

func (m *mockABILoader) loadDeployMsgForInstance(addrHexNo0x string) (*messages.DeployContract, *contractInfo, error) {
	m.capturedAddr = addrHexNo0x
	return m.deployMsg, m.contractInfo, m.loadABIError
}

func (m *mockABILoader) resolveContractAddr(registeredName string) (string, error) {
	return m.registeredContractAddr, m.resolveContractErr
}

func (m *mockABILoader) loadDeployMsgByID(addrHexNo0x string) (*messages.DeployContract, *abiInfo, error) {
	return m.deployMsg, m.abiInfo, m.loadABIError
}

func (m *mockABILoader) checkNameAvailable(name string, isRemote bool) error {
	return m.nameAvailableError
}

func (m *mockABILoader) PreDeploy(msg *messages.DeployContract) error { return nil }
func (m *mockABILoader) PostDeploy(msg *messages.TransactionReceipt) error {
	return m.postDeployError
}
func (m *mockABILoader) AddRoutes(router *httprouter.Router) { return }
func (m *mockABILoader) Shutdown()                           { return }

type mockRPC struct {
	capturedMethod string
	capturedArgs   []interface{}
	mockError      error
	result         interface{}
}

func (m *mockRPC) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	m.capturedMethod = method
	m.capturedArgs = args
	v := reflect.ValueOf(result)
	v.Elem().Set(reflect.ValueOf(m.result))
	return m.mockError
}

type mockSubMgr struct {
	err             error
	updateStreamErr error
	sub             *events.SubscriptionInfo
	stream          *events.StreamInfo
	subs            []*events.SubscriptionInfo
	streams         []*events.StreamInfo
	suspended       bool
	resumed         bool
	capturedAddr    *ethbinding.Address
}

func (m *mockSubMgr) Init() error { return m.err }
func (m *mockSubMgr) AddStream(ctx context.Context, spec *events.StreamInfo) (*events.StreamInfo, error) {
	return spec, m.err
}
func (m *mockSubMgr) UpdateStream(ctx context.Context, id string, spec *events.StreamInfo) (*events.StreamInfo, error) {
	return m.stream, m.updateStreamErr
}
func (m *mockSubMgr) Streams(ctx context.Context) []*events.StreamInfo { return m.streams }
func (m *mockSubMgr) StreamByID(ctx context.Context, id string) (*events.StreamInfo, error) {
	return m.stream, m.err
}
func (m *mockSubMgr) SuspendStream(ctx context.Context, id string) error {
	m.suspended = true
	return m.err
}
func (m *mockSubMgr) ResumeStream(ctx context.Context, id string) error {
	m.resumed = true
	return m.err
}
func (m *mockSubMgr) DeleteStream(ctx context.Context, id string) error { return m.err }
func (m *mockSubMgr) AddSubscription(ctx context.Context, addr *ethbinding.Address, event *ethbinding.ABIElementMarshaling, streamID, initialBlock, name string) (*events.SubscriptionInfo, error) {
	m.capturedAddr = addr
	return m.sub, m.err
}
func (m *mockSubMgr) Subscriptions(ctx context.Context) []*events.SubscriptionInfo { return m.subs }
func (m *mockSubMgr) SubscriptionByID(ctx context.Context, id string) (*events.SubscriptionInfo, error) {
	return m.sub, m.err
}
func (m *mockSubMgr) DeleteSubscription(ctx context.Context, id string) error { return m.err }
func (m *mockSubMgr) ResetSubscription(ctx context.Context, id, initialBlock string) error {
	return m.err
}
func (m *mockSubMgr) Close() {}

func newTestDeployMsg(t *testing.T, addr string) *deployContractWithAddress {
	compiled, err := eth.CompileContract(simpleEventsSource(), "SimpleEvents", "", "")
	assert.NoError(t, err)
	return &deployContractWithAddress{
		DeployContract: messages.DeployContract{ABI: compiled.ABI},
		Address:        addr,
	}
}

func newTestREST2Eth(t *testing.T, dispatcher *mockREST2EthDispatcher) (*rest2eth, *mockRPC, *httprouter.Router) {
	mockRPC := &mockRPC{}
	deployMsg := newTestDeployMsg(t, "")
	abiLoader := &mockABILoader{
		deployMsg: &deployMsg.DeployContract,
	}
	mockProcessor := &mockProcessor{}
	r := newREST2eth(abiLoader, mockRPC, nil, nil, mockProcessor, dispatcher, dispatcher)
	router := &httprouter.Router{}
	r.addRoutes(router)

	return r, mockRPC, router
}

func newTestREST2EthCustomAbiLoader(dispatcher *mockREST2EthDispatcher, abiLoader *mockABILoader) (*rest2eth, *mockRPC, *httprouter.Router) {
	mockRPC := &mockRPC{}
	mockProcessor := &mockProcessor{}
	r := newREST2eth(abiLoader, mockRPC, nil, nil, mockProcessor, dispatcher, dispatcher)
	router := &httprouter.Router{}
	r.addRoutes(router)

	return r, mockRPC, router
}

func newTestREST2EthAndMsg(t *testing.T, dispatcher *mockREST2EthDispatcher, from, to string, bodyMap map[string]interface{}) (*rest2eth, *mockRPC, *httprouter.Router, *httptest.ResponseRecorder, *http.Request) {
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	res := httptest.NewRecorder()

	r, mockRPC, router := newTestREST2Eth(t, dispatcher)

	return r, mockRPC, router, res, req
}

func newTestREST2EthAndMsgPostDeployError(t *testing.T, dispatcher *mockREST2EthDispatcher, from, to string, bodyMap map[string]interface{}) (*rest2eth, *mockRPC, *httprouter.Router, *httptest.ResponseRecorder, *http.Request) {
	deployMsg := newTestDeployMsg(t, "")
	abiLoader := &mockABILoader{
		deployMsg:       &deployMsg.DeployContract,
		postDeployError: fmt.Errorf("pop"),
	}
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	res := httptest.NewRecorder()

	r, mockRPC, router := newTestREST2EthCustomAbiLoader(dispatcher, abiLoader)

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
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, req := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	req.Header.Set("X-Firefly-PrivateFrom", "0xdC416B907857Fa8c0e0d55ec21766Ee3546D5f90")
	req.Header.Set("X-Firefly-PrivateFor", "0xE7E32f0d5A2D55B2aD27E0C2d663807F28f7c745,0xB92F8CebA52fFb5F08f870bd355B1d32f0fd9f7C")
	router.ServeHTTP(res, req)

	assert.Equal(202, res.Result().StatusCode)
	reply := messages.AsyncSentMsg{}
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
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, "", bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1?fly-privateFrom=0xdC416B907857Fa8c0e0d55ec21766Ee3546D5f90&fly-privateFor=0xE7E32f0d5A2D55B2aD27E0C2d663807F28f7c745&fly-privateFor=0xB92F8CebA52fFb5F08f870bd355B1d32f0fd9f7C", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(202, res.Result().StatusCode)
	reply := messages.AsyncSentMsg{}
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

func TestDeployContractAsyncHDWallet(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	from := "HD-u01234abcd-u01234abcd-12345"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, "", bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(202, res.Result().StatusCode)
	reply := messages.AsyncSentMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal(true, reply.Sent)
	assert.Equal("request1", reply.Request)

	assert.Equal(true, dispatcher.asyncDispatchAck)
	assert.Equal(strings.ToLower(from), dispatcher.asyncDispatchMsg["from"])
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
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	r, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, "", bodyMap)
	abiLoader := r.gw.(*mockABILoader)
	abiLoader.nameAvailableError = fmt.Errorf("spent already")
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1?fly-privateFrom=0xdC416B907857Fa8c0e0d55ec21766Ee3546D5f90&fly-privateFor=0xE7E32f0d5A2D55B2aD27E0C2d663807F28f7c745&fly-privateFor=0xB92F8CebA52fFb5F08f870bd355B1d32f0fd9f7C", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	req.Header.Add("x-firefly-register", "random")
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
	receipt := &messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				CommonHeaders: messages.CommonHeaders{
					MsgType: messages.MsgTypeTransactionSuccess,
				},
			},
		},
	}
	dispatcher := &mockREST2EthDispatcher{
		sendTransactionSyncReceipt: receipt,
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?fly-sync&fly-ethvalue=1234", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(json.Number("1234"), dispatcher.sendTransactionMsg.Value)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)
}

func TestSendTransactionSyncFailure(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	receipt := &messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				CommonHeaders: messages.CommonHeaders{
					MsgType: messages.MsgTypeTransactionFailure,
				},
			},
		},
	}
	dispatcher := &mockREST2EthDispatcher{
		sendTransactionSyncReceipt: receipt,
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?fly-sync&fly-ethvalue=1234", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(json.Number("1234"), dispatcher.sendTransactionMsg.Value)

	assert.Equal(500, res.Result().StatusCode)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)
}

func TestSendTransactionSyncPostDeployErr(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	contractAddr := ethbind.API.HexToAddress("0x0123456789AbcdeF0123456789abCdef01234567")
	receipt := &messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				CommonHeaders: messages.CommonHeaders{
					MsgType: messages.MsgTypeTransactionSuccess,
				},
			},
		},
		ContractAddress: &contractAddr,
	}
	dispatcher := &mockREST2EthDispatcher{
		sendTransactionSyncReceipt: receipt,
	}
	_, _, router, res, _ := newTestREST2EthAndMsgPostDeployError(t, dispatcher, from, to, bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?fly-sync&fly-ethvalue=1234", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(json.Number("1234"), dispatcher.sendTransactionMsg.Value)

	assert.Equal(500, res.Result().StatusCode)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)
	assert.Equal(contractAddr, *dispatcher.sendTransactionSyncReceipt.ContractAddress)
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
	receipt := &messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				CommonHeaders: messages.CommonHeaders{
					MsgType: messages.MsgTypeTransactionSuccess,
				},
			},
		},
	}
	dispatcher := &mockREST2EthDispatcher{
		sendTransactionSyncReceipt: receipt,
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/"+abi+"/"+to+"/set?fly-sync&fly-ethvalue=1234", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
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
	receipt := &messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				CommonHeaders: messages.CommonHeaders{
					MsgType: messages.MsgTypeTransactionSuccess,
				},
			},
		},
	}
	dispatcher := &mockREST2EthDispatcher{
		deployContractSyncReceipt: receipt,
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, "", bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1?fly-sync", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(from, dispatcher.deployContractMsg.From)
}

func TestDeployContractSyncRemoteRegitryInstance(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	receipt := &messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				CommonHeaders: messages.CommonHeaders{
					MsgType: messages.MsgTypeTransactionSuccess,
					Context: map[string]interface{}{
						remoteRegistryContextKey: true,
					},
				},
			},
		},
	}
	dispatcher := &mockREST2EthDispatcher{
		sendTransactionSyncReceipt: receipt,
	}
	r, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, "", bodyMap)
	r.rr = &mockRR{
		deployMsg: newTestDeployMsg(t, strings.TrimPrefix(to, "0x")),
	}
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/instances/myinstance/set?fly-sync", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)
}

func TestDeployContractSyncRemoteRegitryInstance500(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	r, _, router, res, _ := newTestREST2EthAndMsg(t, &mockREST2EthDispatcher{}, "", "", bodyMap)
	r.rr = &mockRR{
		err: fmt.Errorf("pop"),
	}
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/instances/myinstance/set?fly-sync", bytes.NewReader(body))
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
}

func TestDeployContractSyncRemoteRegitryInstance404(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	r, _, router, res, _ := newTestREST2EthAndMsg(t, &mockREST2EthDispatcher{}, "", "", bodyMap)
	r.rr = &mockRR{}
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/instances/myinstance/set?fly-sync", bytes.NewReader(body))
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
}

func TestDeployContractSyncRemoteRegitryGateway500(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	r, _, router, res, _ := newTestREST2EthAndMsg(t, &mockREST2EthDispatcher{}, "", "", bodyMap)
	r.rr = &mockRR{
		err: fmt.Errorf("pop"),
	}
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/g/mygw?fly-sync", bytes.NewReader(body))
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
}

func TestDeployContractSyncRemoteRegitryGateway404(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	r, _, router, res, _ := newTestREST2EthAndMsg(t, &mockREST2EthDispatcher{}, "", "", bodyMap)
	r.rr = &mockRR{}
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/g/mygw?fly-sync", bytes.NewReader(body))
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
}

func TestDeployContractSyncRemoteRegistryGateway(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	receipt := &messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				CommonHeaders: messages.CommonHeaders{
					MsgType: messages.MsgTypeTransactionSuccess,
				},
			},
		},
	}
	dispatcher := &mockREST2EthDispatcher{
		sendTransactionSyncReceipt: receipt,
	}
	r, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, "", bodyMap)
	r.rr = &mockRR{
		deployMsg: newTestDeployMsg(t, strings.TrimPrefix(to, "0x")),
	}
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/g/mygateway/567a417717cb6c59ddc1035705f02c0fd1ab1872/set?fly-sync", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)
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
	_, _, router, res, req := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	req.Header.Set("x-firefly-sync", "true")
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
	_, _, router, res, req := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
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
	_, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, "", bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
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
	_, _, router, res, req := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
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
	_, _, router, res, req := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
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
	r, _, router, res, req := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
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
	_, _, router, res, req := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Please specify a valid address in the 'fly-from' query string parameter or x-firefly-from HTTP header", reply.Message)
}

func TestSendTransactionBadMethodABI(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	abiLoader := &mockABILoader{
		deployMsg: &messages.DeployContract{
			ABI: ethbinding.ABIMarshaling{
				{
					Name: "badmethod", Type: "function", Inputs: []ethbinding.ABIArgumentMarshaling{
						{Name: "badness", Type: "badness"},
					},
				},
			},
		},
	}
	_, _, router := newTestREST2EthCustomAbiLoader(dispatcher, abiLoader)
	req := httptest.NewRequest("GET", "/contracts/0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8/badmethod", bytes.NewReader([]byte{}))
	req.Header.Add("x-firefly-from", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Invalid method 'badmethod' in ABI: unsupported arg type: badness", reply.Message)
}

func TestSendTransactionBadEventABI(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	abiLoader := &mockABILoader{
		deployMsg: &messages.DeployContract{
			ABI: ethbinding.ABIMarshaling{
				{
					Name: "badevent", Type: "event", Inputs: []ethbinding.ABIArgumentMarshaling{
						{Name: "badness", Type: "badness"},
					},
				},
			},
		},
	}
	_, _, router := newTestREST2EthCustomAbiLoader(dispatcher, abiLoader)
	req := httptest.NewRequest("POST", "/contracts/0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8/badevent/subscribe", bytes.NewReader([]byte{}))
	req.Header.Add("x-firefly-from", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Invalid event 'badevent' in ABI: unsupported arg type: badness", reply.Message)
}

func TestSendTransactionBadConstructorABI(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	abiLoader := &mockABILoader{
		deployMsg: &messages.DeployContract{
			ABI: ethbinding.ABIMarshaling{
				{
					Name: "badevent", Type: "constructor", Inputs: []ethbinding.ABIArgumentMarshaling{
						{Name: "badness", Type: "badness"},
					},
				},
			},
		},
	}
	_, _, router := newTestREST2EthCustomAbiLoader(dispatcher, abiLoader)
	req := httptest.NewRequest("POST", "/abis/testabi", bytes.NewReader([]byte{}))
	req.Header.Add("x-firefly-from", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Invalid method 'constructor' in ABI: unsupported arg type: badness", reply.Message)
}

func TestSendTransactionDefaultConstructorABI(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	abiLoader := &mockABILoader{
		deployMsg: &messages.DeployContract{
			ABI: ethbinding.ABIMarshaling{}, // completely empty ABI is ok
		},
	}
	_, _, router := newTestREST2EthCustomAbiLoader(dispatcher, abiLoader)
	req := httptest.NewRequest("POST", "/abis/testabi", bytes.NewReader([]byte{}))
	req.Header.Add("x-firefly-from", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(202, res.Result().StatusCode)
}

func TestSendTransactionUnnamedParamsABI(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	abiLoader := &mockABILoader{
		deployMsg: &messages.DeployContract{
			ABI: ethbinding.ABIMarshaling{
				{
					Name: "unnamedparamsmethod", Type: "function", Inputs: []ethbinding.ABIArgumentMarshaling{
						{Name: "", Type: "uint256", InternalType: "uint256"},
						{Name: "", Type: "uint256", InternalType: "uint256"},
					},
				},
			},
		},
	}
	_, _, router := newTestREST2EthCustomAbiLoader(dispatcher, abiLoader)
	req := httptest.NewRequest("POST", "/abis/testabi/0x29fb3f4f7cc82a1456903a506e88cdd63b1d74e8/unnamedparamsmethod", bytes.NewReader([]byte{}))
	req.Header.Add("x-firefly-from", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")
	q := req.URL.Query()
	q.Add("input", "105")
	q.Add("input1", "106")
	req.URL.RawQuery = q.Encode()
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(202, res.Result().StatusCode)
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
	_, _, router, res, req := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
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
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	r, _, router, res, req := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	abiLoader := r.gw.(*mockABILoader)
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
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	r, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, "", bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1?fly-sync", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	abiLoader := r.gw.(*mockABILoader)
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
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/shazaam", bytes.NewReader([]byte("{}")))
	req.Header.Set("x-firefly-from", from)
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
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?i=999&s=msg&fly-ethvalue=12345", bytes.NewReader([]byte("{}")))
	req.Header.Set("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(202, res.Result().StatusCode)
}

func TestSendTransactionRegisteredName(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	to := "transponster"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	r, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	abiLoader := r.gw.(*mockABILoader)
	abiLoader.registeredContractAddr = "c6c572a18d31ff36d661d680c0060307e038dc47"
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?i=999&s=msg&fly-ethvalue=12345", bytes.NewReader([]byte("{}")))
	req.Header.Set("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal("c6c572a18d31ff36d661d680c0060307e038dc47", abiLoader.capturedAddr)
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
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, req := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
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
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?x=999", bytes.NewReader([]byte(":not json or yaml")))
	req.Header.Set("x-firefly-from", from)
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
	_, mockRPC, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, "", to, map[string]interface{}{})
	req := httptest.NewRequest("GET", "/contracts/"+to+"/get", bytes.NewReader([]byte{}))
	mockRPC.result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("eth_call", mockRPC.capturedMethod)
	assert.Equal("latest", mockRPC.capturedArgs[1])
	var reply map[string]interface{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	log.Infof("Reply: %+v", reply)
	assert.Nil(reply["error"])
	assert.Equal("123456", reply["i"])
	assert.Equal("testing", reply["s"])
}

func TestCallMethodHDWalletSuccess(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}
	from := "HD-u01234abcd-u01234abcd-12345"
	r, mockRPC, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, "", to, map[string]interface{}{})
	r.processor.(*mockProcessor).resolvedFrom = "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	req := httptest.NewRequest("GET", "/contracts/"+to+"/get?fly-from="+from, bytes.NewReader([]byte{}))
	mockRPC.result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("eth_call", mockRPC.capturedMethod)
	assert.Equal("latest", mockRPC.capturedArgs[1])
	var reply map[string]interface{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	log.Infof("Reply: %+v", reply)
	assert.Nil(reply["error"])
	assert.Equal("123456", reply["i"])
	assert.Equal("testing", reply["s"])
}

func TestCallMethodHDWalletFail(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}
	from := "HD-u01234abcd-u01234abcd-12345"
	r, mockRPC, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, "", to, map[string]interface{}{})
	r.processor.(*mockProcessor).resolvedFrom = "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	r.processor.(*mockProcessor).err = fmt.Errorf("pop")
	req := httptest.NewRequest("GET", "/contracts/"+to+"/get?fly-from="+from, bytes.NewReader([]byte{}))
	mockRPC.result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	var reply map[string]interface{}
	json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.Equal("pop", reply["error"])
}

func TestCallReadOnlyMethodViaPOSTSuccess(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}
	_, mockRPC, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, "", to, map[string]interface{}{})
	req := httptest.NewRequest("POST", "/contracts/"+to+"/get?fly-blocknumber=12345", bytes.NewReader([]byte{}))
	mockRPC.result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("eth_call", mockRPC.capturedMethod)
	assert.Equal("0x3039", mockRPC.capturedArgs[1])
	var reply map[string]interface{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	log.Infof("Reply: %+v", reply)
	assert.Nil(reply["error"])
	assert.Equal("123456", reply["i"])
	assert.Equal("testing", reply["s"])

	to = "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher = &mockREST2EthDispatcher{}
	_, mockRPC, router, res, _ = newTestREST2EthAndMsg(t, dispatcher, "", to, map[string]interface{}{})
	req = httptest.NewRequest("POST", "/contracts/"+to+"/get?fly-blocknumber=0xab1234", bytes.NewReader([]byte{}))
	mockRPC.result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("eth_call", mockRPC.capturedMethod)
	assert.Equal("0xab1234", mockRPC.capturedArgs[1])

	to = "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher = &mockREST2EthDispatcher{}
	_, mockRPC, router, res, _ = newTestREST2EthAndMsg(t, dispatcher, "", to, map[string]interface{}{})
	req = httptest.NewRequest("POST", "/contracts/"+to+"/get?fly-blocknumber=pending", bytes.NewReader([]byte{}))
	mockRPC.result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("eth_call", mockRPC.capturedMethod)
	assert.Equal("pending", mockRPC.capturedArgs[1])
}

func TestCallMethodFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}
	_, mockRPC, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, "", to, map[string]interface{}{})
	mockRPC.result = ""
	mockRPC.mockError = fmt.Errorf("pop")
	req := httptest.NewRequest("GET", "/contracts/"+to+"/get", bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Regexp("Call failed: pop", reply.Message)

	to = "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher = &mockREST2EthDispatcher{}
	_, mockRPC, router, res, _ = newTestREST2EthAndMsg(t, dispatcher, "", to, map[string]interface{}{})
	req = httptest.NewRequest("POST", "/contracts/"+to+"/get?fly-blocknumber=ab1234", bytes.NewReader([]byte{}))
	mockRPC.result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
	router.ServeHTTP(res, req)
	assert.Equal(500, res.Result().StatusCode)
}

func TestCallMethodViaABIBadAddress(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "badness"
	dispatcher := &mockREST2EthDispatcher{}
	_, _, router, res, _ := newTestREST2EthAndMsg(t, dispatcher, "", to, map[string]interface{}{})
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
	_, _, router := newTestREST2Eth(t, dispatcher)
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
	_, _, router := newTestREST2Eth(t, dispatcher)
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

func TestSubscribeUnauthorized(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	auth.RegisterSecurityModule(&authtest.TestSecurityModule{})

	dispatcher := &mockREST2EthDispatcher{}
	_, _, router := newTestREST2Eth(t, dispatcher)
	bodyBytes, _ := json.Marshal(&map[string]string{
		"stream": "stream1",
	})
	req := httptest.NewRequest("POST", "/abis/ABI1/Changed/subscribe", bytes.NewReader(bodyBytes))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(401, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Unauthorized", reply.Message)

	auth.RegisterSecurityModule(nil)
}

func TestSubscribeNoAddressMissingStream(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, _, router := newTestREST2Eth(t, dispatcher)
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
	r, _, router := newTestREST2Eth(t, dispatcher)
	sm := &mockSubMgr{
		sub: &events.SubscriptionInfo{ID: "sub1", Name: "stream-without-address"},
	}
	r.subMgr = sm
	bodyBytes, _ := json.Marshal(&map[string]string{
		"stream": "stream1",
	})
	req := httptest.NewRequest("POST", "/abis/ABI1/Changed/subscribe", bytes.NewReader(bodyBytes))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	reply := events.SubscriptionInfo{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("sub1", reply.ID)
	assert.Equal("stream-without-address", reply.Name)
	assert.Nil(sm.capturedAddr)
}

func TestSubscribeWithAddressSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, _, router := newTestREST2Eth(t, dispatcher)
	sm := &mockSubMgr{
		sub: &events.SubscriptionInfo{ID: "sub1"},
	}
	r.subMgr = sm
	bodyBytes, _ := json.Marshal(&map[string]string{
		"stream": "stream1",
	})
	req := httptest.NewRequest("POST", "/contracts/0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8/Changed/subscribe", bytes.NewReader(bodyBytes))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	reply := events.SubscriptionInfo{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("sub1", reply.ID)
	assert.Equal("0x66C5fE653e7A9EBB628a6D40f0452d1e358BaEE8", sm.capturedAddr.Hex())
}

func TestSubscribeWithAddressBadAddress(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, _, router := newTestREST2Eth(t, dispatcher)
	abiLoader := r.gw.(*mockABILoader)
	abiLoader.resolveContractErr = fmt.Errorf("unregistered")
	r.subMgr = &mockSubMgr{
		sub: &events.SubscriptionInfo{ID: "sub1"},
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
	r, _, router := newTestREST2Eth(t, dispatcher)
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

func TestSendTransactionWithIDAsyncSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})
	bodyMap["i"] = 12345
	bodyMap["s"] = "testing"
	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	from := "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	dispatcher := &mockREST2EthDispatcher{
		asyncDispatchReply: &messages.AsyncSentMsg{
			Sent:    true,
			Request: "request1",
		},
	}
	_, _, router, res, req := newTestREST2EthAndMsg(t, dispatcher, from, to, bodyMap)
	req.Header.Set("X-Firefly-PrivateFrom", "0xdC416B907857Fa8c0e0d55ec21766Ee3546D5f90")
	req.Header.Set("X-Firefly-PrivateFor", "0xE7E32f0d5A2D55B2aD27E0C2d663807F28f7c745,0xB92F8CebA52fFb5F08f870bd355B1d32f0fd9f7C")
	req.Header.Set("X-Firefly-ID", "my-id")
	router.ServeHTTP(res, req)

	assert.Equal(202, res.Result().StatusCode)
	reply := messages.AsyncSentMsg{}
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
	assert.Equal("my-id", dispatcher.asyncDispatchMsg["headers"].(map[string]interface{})["id"])
}
