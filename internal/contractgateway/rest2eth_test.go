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

package contractgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperledger-labs/firefly-ethconnect/internal/auth"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/auth/authtest"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/contractregistry"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/eth"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/ethbind"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/events"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/messages"
	"github.com/hyperledger-labs/firefly-ethconnect/mocks/contractregistrymocks"
	"github.com/hyperledger-labs/firefly-ethconnect/mocks/ethmocks"
	"github.com/julienschmidt/httprouter"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

type mockGateway struct {
	postDeployError error
}

func (m *mockGateway) SendReply(message interface{}) {
}
func (m *mockGateway) PreDeploy(msg *messages.DeployContract) error { return nil }
func (m *mockGateway) PostDeploy(msg *messages.TransactionReceipt) error {
	return m.postDeployError
}
func (m *mockGateway) AddRoutes(router *httprouter.Router) { return }
func (m *mockGateway) Shutdown()                           { return }

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

func newTestDeployMsg(t *testing.T, addr string) *contractregistry.DeployContractWithAddress {
	compiled, err := eth.CompileContract(simpleEventsSource(), "SimpleEvents", "", "")
	assert.NoError(t, err)
	return &contractregistry.DeployContractWithAddress{
		DeployContract: messages.DeployContract{ABI: compiled.ABI},
		Address:        addr,
	}
}

func newTestREST2Eth(dispatcher *mockREST2EthDispatcher) (*rest2eth, *httprouter.Router) {
	mockRPC := &ethmocks.RPCClient{}
	gateway := &mockGateway{}
	contractResolver := &contractregistrymocks.ContractStore{}
	remoteRegistry := &contractregistrymocks.RemoteRegistry{}
	mockProcessor := &mockProcessor{}
	r := newREST2eth(gateway, contractResolver, mockRPC, nil, remoteRegistry, mockProcessor, dispatcher, dispatcher)
	router := &httprouter.Router{}
	r.addRoutes(router)

	return r, router
}

func newTestREST2EthAndMsg(dispatcher *mockREST2EthDispatcher, from, to string, bodyMap map[string]interface{}) (*rest2eth, *httprouter.Router, *httptest.ResponseRecorder, *http.Request) {
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	res := httptest.NewRecorder()

	r, router := newTestREST2Eth(dispatcher)
	return r, router, res, req
}

func expectContractSuccess(t *testing.T, mcr *contractregistrymocks.ContractStore, address string) {
	mcr.On("GetContractByAddress", address).Return(&contractregistry.ContractInfo{ABI: "abi1"}, nil)
	expectABISuccess(t, mcr, "abi1")
}

func expectABISuccess(t *testing.T, mcr *contractregistrymocks.ContractStore, abiID string) {
	mcr.On("GetABIByID", abiID).Return(&newTestDeployMsg(t, "").DeployContract, nil, nil)
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

	r, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

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

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, "abi1")

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

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, "abi1")

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

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, "abi1")
	mcr.On("CheckNameAvailable", "random", false).Return(fmt.Errorf("spent already"))

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1?fly-privateFrom=0xdC416B907857Fa8c0e0d55ec21766Ee3546D5f90&fly-privateFor=0xE7E32f0d5A2D55B2aD27E0C2d663807F28f7c745&fly-privateFor=0xB92F8CebA52fFb5F08f870bd355B1d32f0fd9f7C", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	req.Header.Add("x-firefly-register", "random")
	router.ServeHTTP(res, req)

	assert.Equal(409, res.Result().StatusCode)
	var resBody map[string]interface{}
	json.NewDecoder(res.Body).Decode(&resBody)
	assert.Equal("spent already", resBody["error"])

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?fly-sync&fly-ethvalue=1234", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(json.Number("1234"), dispatcher.sendTransactionMsg.Value)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?fly-sync&fly-ethvalue=1234", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(json.Number("1234"), dispatcher.sendTransactionMsg.Value)

	assert.Equal(500, res.Result().StatusCode)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)

	mcr.AssertExpectations(t)
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

	r, router := newTestREST2Eth(dispatcher)
	r.gw = &mockGateway{
		postDeployError: fmt.Errorf("pop"),
	}
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?fly-sync&fly-ethvalue=1234", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(json.Number("1234"), dispatcher.sendTransactionMsg.Value)

	assert.Equal(500, res.Result().StatusCode)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)
	assert.Equal(contractAddr, *dispatcher.sendTransactionSyncReceipt.ContractAddress)

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, abi)

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/"+abi+"/"+to+"/set?fly-sync&fly-ethvalue=1234", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(json.Number("1234"), dispatcher.sendTransactionMsg.Value)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, "abi1")

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1?fly-sync", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(from, dispatcher.deployContractMsg.From)

	mcr.AssertExpectations(t)
}

func TestDeployContractSyncRemoteRegistryInstance(t *testing.T) {
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
						contractregistry.RemoteRegistryContextKey: true,
					},
				},
			},
		},
	}
	dispatcher := &mockREST2EthDispatcher{
		sendTransactionSyncReceipt: receipt,
	}

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	mrr := r.rr.(*contractregistrymocks.RemoteRegistry)
	mrr.On("LoadFactoryForInstance", "myinstance", false).
		Return(newTestDeployMsg(t, strings.TrimPrefix(to, "0x")), nil)

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/instances/myinstance/set?fly-sync", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)

	mrr.AssertExpectations(t)
}

func TestDeployContractSyncRemoteRegistryInstance500(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})

	r, router, res, _ := newTestREST2EthAndMsg(&mockREST2EthDispatcher{}, "", "", bodyMap)
	mrr := r.rr.(*contractregistrymocks.RemoteRegistry)
	mrr.On("LoadFactoryForInstance", "myinstance", false).Return(nil, fmt.Errorf("pop"))

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/instances/myinstance/set?fly-sync", bytes.NewReader(body))
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)

	mrr.AssertExpectations(t)
}

func TestDeployContractSyncRemoteRegistryInstance404(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})

	r, router, res, _ := newTestREST2EthAndMsg(&mockREST2EthDispatcher{}, "", "", bodyMap)
	mrr := r.rr.(*contractregistrymocks.RemoteRegistry)
	mrr.On("LoadFactoryForInstance", "myinstance", false).Return(nil, nil)

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/instances/myinstance/set?fly-sync", bytes.NewReader(body))
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)

	mrr.AssertExpectations(t)
}

func TestDeployContractSyncRemoteRegistryGateway500(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})

	r, router, res, _ := newTestREST2EthAndMsg(&mockREST2EthDispatcher{}, "", "", bodyMap)
	mrr := r.rr.(*contractregistrymocks.RemoteRegistry)
	mrr.On("LoadFactoryForGateway", "mygw", false).Return(nil, fmt.Errorf("pop"))

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/g/mygw?fly-sync", bytes.NewReader(body))
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)

	mrr.AssertExpectations(t)
}

func TestDeployContractSyncRemoteRegistryGateway404(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	bodyMap := make(map[string]interface{})

	r, router, res, _ := newTestREST2EthAndMsg(&mockREST2EthDispatcher{}, "", "", bodyMap)
	mrr := r.rr.(*contractregistrymocks.RemoteRegistry)
	mrr.On("LoadFactoryForGateway", "mygw", false).Return(nil, nil)

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/g/mygw?fly-sync", bytes.NewReader(body))
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)

	mrr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	mrr := r.rr.(*contractregistrymocks.RemoteRegistry)
	mrr.On("LoadFactoryForGateway", "mygateway", false).
		Return(&newTestDeployMsg(t, strings.TrimPrefix(to, "0x")).DeployContract, nil)

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/g/mygateway/567a417717cb6c59ddc1035705f02c0fd1ab1872/set?fly-sync", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(to, dispatcher.sendTransactionMsg.To)
	assert.Equal(from, dispatcher.sendTransactionMsg.From)

	mrr.AssertExpectations(t)
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

	r, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	req.Header.Set("x-firefly-sync", "true")
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, "abi1")

	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("ResolveContractAddress", "badness").Return("my-contract", nil)
	expectContractSuccess(t, mcr, "my-contract")

	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("ResolveContractAddress", "random").Return("", fmt.Errorf("unregistered"))

	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("unregistered", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Please specify a valid address in the 'fly-from' query string parameter or x-firefly-from HTTP header", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("GetContractByAddress", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8").
		Return(&contractregistry.ContractInfo{ABI: "abi-id"}, nil)
	mcr.On("GetABIByID", "abi-id").
		Return(&messages.DeployContract{
			ABI: ethbinding.ABIMarshaling{
				{
					Name: "badmethod", Type: "function", Inputs: []ethbinding.ABIArgumentMarshaling{
						{Name: "badness", Type: "badness"},
					},
				},
			},
		}, &contractregistry.ABIInfo{}, nil)

	req := httptest.NewRequest("GET", "/contracts/0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8/badmethod", bytes.NewReader([]byte{}))
	req.Header.Add("x-firefly-from", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Invalid method 'badmethod' in ABI: unsupported arg type: badness", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("GetContractByAddress", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8").
		Return(&contractregistry.ContractInfo{ABI: "abi-id"}, nil)
	mcr.On("GetABIByID", "abi-id").
		Return(&messages.DeployContract{
			ABI: ethbinding.ABIMarshaling{
				{
					Name: "badevent", Type: "event", Inputs: []ethbinding.ABIArgumentMarshaling{
						{Name: "badness", Type: "badness"},
					},
				},
			},
		}, &contractregistry.ABIInfo{}, nil)

	req := httptest.NewRequest("POST", "/contracts/0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8/badevent/subscribe", bytes.NewReader([]byte{}))
	req.Header.Add("x-firefly-from", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Invalid event 'badevent' in ABI: unsupported arg type: badness", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("GetABIByID", "testabi").
		Return(&messages.DeployContract{
			ABI: ethbinding.ABIMarshaling{
				{
					Name: "badevent", Type: "constructor", Inputs: []ethbinding.ABIArgumentMarshaling{
						{Name: "badness", Type: "badness"},
					},
				},
			},
		}, &contractregistry.ABIInfo{}, nil)

	req := httptest.NewRequest("POST", "/abis/testabi", bytes.NewReader([]byte{}))
	req.Header.Add("x-firefly-from", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Invalid method 'constructor' in ABI: unsupported arg type: badness", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("GetABIByID", "testabi").
		Return(&messages.DeployContract{
			ABI: ethbinding.ABIMarshaling{}, // completely empty ABI is ok
		}, &contractregistry.ABIInfo{}, nil)

	req := httptest.NewRequest("POST", "/abis/testabi", bytes.NewReader([]byte{}))
	req.Header.Add("x-firefly-from", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(202, res.Result().StatusCode)

	mcr.AssertExpectations(t)
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

	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("GetABIByID", "testabi").
		Return(&messages.DeployContract{
			ABI: ethbinding.ABIMarshaling{
				{
					Name: "unnamedparamsmethod", Type: "function", Inputs: []ethbinding.ABIArgumentMarshaling{
						{Name: "", Type: "uint256", InternalType: "uint256"},
						{Name: "", Type: "uint256", InternalType: "uint256"},
					},
				},
			},
		}, &contractregistry.ABIInfo{}, nil)

	req := httptest.NewRequest("POST", "/abis/testabi/0x29fb3f4f7cc82a1456903a506e88cdd63b1d74e8/unnamedparamsmethod", bytes.NewReader([]byte{}))
	req.Header.Add("x-firefly-from", "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")
	q := req.URL.Query()
	q.Add("input", "105")
	q.Add("input1", "106")
	req.URL.RawQuery = q.Encode()
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(202, res.Result().StatusCode)

	mcr.AssertExpectations(t)
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

	r, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("From Address must be a 40 character hex string (0x prefix is optional)", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("GetContractByAddress", to).Return(nil, fmt.Errorf("pop"))

	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, "", bodyMap)
	body, _ := json.Marshal(&bodyMap)
	req := httptest.NewRequest("POST", "/abis/abi1?fly-sync", bytes.NewReader(body))
	req.Header.Add("x-firefly-from", from)

	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("GetABIByID", "abi1").Return(nil, nil, fmt.Errorf("pop"))

	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("pop", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	req := httptest.NewRequest("POST", "/contracts/"+to+"/shazaam", bytes.NewReader([]byte("{}")))
	req.Header.Set("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Method or Event 'shazaam' is not declared in the ABI of contract '567a417717cb6c59ddc1035705f02c0fd1ab1872'", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?i=999&s=msg&fly-ethvalue=12345", bytes.NewReader([]byte("{}")))
	req.Header.Set("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(202, res.Result().StatusCode)

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("ResolveContractAddress", "transponster").Return("c6c572a18d31ff36d661d680c0060307e038dc47", nil)
	expectContractSuccess(t, mcr, "c6c572a18d31ff36d661d680c0060307e038dc47")

	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?i=999&s=msg&fly-ethvalue=12345", bytes.NewReader([]byte("{}")))
	req.Header.Set("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(202, res.Result().StatusCode)

	mcr.AssertExpectations(t)
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

	r, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("Parameter 'i' of method 'set' was not specified in body or query parameters", reply.Message)

	mcr.AssertExpectations(t)
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

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	req := httptest.NewRequest("POST", "/contracts/"+to+"/set?x=999", bytes.NewReader([]byte(":not json or yaml")))
	req.Header.Set("x-firefly-from", from)
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Regexp("Unable to parse as YAML or JSON", reply.Message)

	mcr.AssertExpectations(t)
}

func TestCallMethodSuccess(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	mockRPC := r.rpc.(*ethmocks.RPCClient)
	mockRPC.On("CallContext", mock.Anything, mock.Anything, "eth_call", mock.Anything, "latest").
		Run(func(args mock.Arguments) {
			result := args[1].(*string)
			*result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
		}).
		Return(nil)

	req := httptest.NewRequest("GET", "/contracts/"+to+"/get", bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	var reply map[string]interface{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	log.Infof("Reply: %+v", reply)
	assert.Nil(reply["error"])
	assert.Equal("123456", reply["i"])
	assert.Equal("testing", reply["s"])

	mcr.AssertExpectations(t)
	mockRPC.AssertExpectations(t)
}

func TestCallMethodHDWalletSuccess(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}
	from := "HD-u01234abcd-u01234abcd-12345"

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	mockRPC := r.rpc.(*ethmocks.RPCClient)
	mockRPC.On("CallContext", mock.Anything, mock.Anything, "eth_call", mock.Anything, "latest").
		Run(func(args mock.Arguments) {
			result := args[1].(*string)
			*result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
		}).
		Return(nil)

	r.processor.(*mockProcessor).resolvedFrom = "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	req := httptest.NewRequest("GET", "/contracts/"+to+"/get?fly-from="+from, bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	var reply map[string]interface{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	log.Infof("Reply: %+v", reply)
	assert.Nil(reply["error"])
	assert.Equal("123456", reply["i"])
	assert.Equal("testing", reply["s"])

	mcr.AssertExpectations(t)
	mockRPC.AssertExpectations(t)
}

func TestCallMethodHDWalletFail(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}
	from := "HD-u01234abcd-u01234abcd-12345"

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	r.processor.(*mockProcessor).resolvedFrom = "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8"
	r.processor.(*mockProcessor).err = fmt.Errorf("pop")
	req := httptest.NewRequest("GET", "/contracts/"+to+"/get?fly-from="+from, bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	var reply map[string]interface{}
	json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.Equal("pop", reply["error"])

	mcr.AssertExpectations(t)
}

func TestCallReadOnlyMethodViaPOSTSuccess(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	mockRPC := r.rpc.(*ethmocks.RPCClient)
	mockRPC.On("CallContext", mock.Anything, mock.Anything, "eth_call", mock.Anything, "0x3039").
		Run(func(args mock.Arguments) {
			result := args[1].(*string)
			*result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
		}).
		Return(nil)

	req := httptest.NewRequest("POST", "/contracts/"+to+"/get?fly-blocknumber=12345", bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	var reply map[string]interface{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	log.Infof("Reply: %+v", reply)
	assert.Nil(reply["error"])
	assert.Equal("123456", reply["i"])
	assert.Equal("testing", reply["s"])

	mcr.AssertExpectations(t)
	mockRPC.AssertExpectations(t)

	to = "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher = &mockREST2EthDispatcher{}

	r, router, res, _ = newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	mcr = r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	mockRPC = r.rpc.(*ethmocks.RPCClient)
	mockRPC.On("CallContext", mock.Anything, mock.Anything, "eth_call", mock.Anything, "0xab1234").
		Run(func(args mock.Arguments) {
			result := args[1].(*string)
			*result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
		}).
		Return(nil)

	req = httptest.NewRequest("POST", "/contracts/"+to+"/get?fly-blocknumber=0xab1234", bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)

	mcr.AssertExpectations(t)
	mockRPC.AssertExpectations(t)

	to = "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher = &mockREST2EthDispatcher{}

	r, router, res, _ = newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	mcr = r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	mockRPC = r.rpc.(*ethmocks.RPCClient)
	mockRPC.On("CallContext", mock.Anything, mock.Anything, "eth_call", mock.Anything, "pending").
		Run(func(args mock.Arguments) {
			result := args[1].(*string)
			*result = "0x000000000000000000000000000000000000000000000000000000000001e2400000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000000774657374696e6700000000000000000000000000000000000000000000000000"
		}).
		Return(nil)

	req = httptest.NewRequest("POST", "/contracts/"+to+"/get?fly-blocknumber=pending", bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)

	mcr.AssertExpectations(t)
	mockRPC.AssertExpectations(t)
}

func TestCallMethodFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher := &mockREST2EthDispatcher{}

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	mockRPC := r.rpc.(*ethmocks.RPCClient)
	mockRPC.On("CallContext", mock.Anything, mock.Anything, "eth_call", mock.Anything, "latest").
		Return(fmt.Errorf("pop"))

	req := httptest.NewRequest("GET", "/contracts/"+to+"/get", bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Regexp("Call failed: pop", reply.Message)

	mcr.AssertExpectations(t)
	mockRPC.AssertExpectations(t)

	to = "0x567a417717cb6c59ddc1035705f02c0fd1ab1872"
	dispatcher = &mockREST2EthDispatcher{}

	r, router, res, _ = newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	mcr = r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

	req = httptest.NewRequest("POST", "/contracts/"+to+"/get?fly-blocknumber=ab1234", bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)
	assert.Equal(500, res.Result().StatusCode)

	mcr.AssertExpectations(t)
}

func TestCallMethodViaABIBadAddress(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	to := "badness"
	dispatcher := &mockREST2EthDispatcher{}

	r, router, res, _ := newTestREST2EthAndMsg(dispatcher, "", to, map[string]interface{}{})
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, "ABI1")

	req := httptest.NewRequest("GET", "/abis/ABI1/badaddress/get", bytes.NewReader([]byte{}))
	router.ServeHTTP(res, req)

	assert.Equal(404, res.Result().StatusCode)
	reply := restErrMsg{}
	err := json.NewDecoder(res.Result().Body).Decode(&reply)
	assert.NoError(err)
	assert.Equal("To Address must be a 40 character hex string (0x prefix is optional)", reply.Message)

	mcr.AssertExpectations(t)
}

func TestSubscribeNoAddressNoSubMgr(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}

	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, "ABI1")

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

	mcr.AssertExpectations(t)
}

func TestSubscribeNoAddressUnknownEvent(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}

	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, "ABI1")

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

	mcr.AssertExpectations(t)
}

func TestSubscribeUnauthorized(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	auth.RegisterSecurityModule(&authtest.TestSecurityModule{})

	dispatcher := &mockREST2EthDispatcher{}
	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, "ABI1")

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
	mcr.AssertExpectations(t)
}

func TestSubscribeNoAddressMissingStream(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, "ABI1")

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

	mcr.AssertExpectations(t)
}

func TestSubscribeNoAddressSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectABISuccess(t, mcr, "ABI1")

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

	mcr.AssertExpectations(t)
}

func TestSubscribeWithAddressSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")

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

	mcr.AssertExpectations(t)
}

func TestSubscribeWithAddressBadAddress(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}

	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("ResolveContractAddress", "badness").Return("", fmt.Errorf("unregistered"))

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

	mcr.AssertExpectations(t)
}

func TestSubscribeWithAddressSubmgrFailure(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, router := newTestREST2Eth(dispatcher)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, "0x66c5fe653e7a9ebb628a6d40f0452d1e358baee8")

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

	mcr.AssertExpectations(t)
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

	r, router, res, req := newTestREST2EthAndMsg(dispatcher, from, to, bodyMap)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	expectContractSuccess(t, mcr, to)

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
	assert.Equal("my-id", dispatcher.asyncDispatchMsg["headers"].(map[string]interface{})["id"])

	mcr.AssertExpectations(t)
}

func TestGetTransactionInfoSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	gas := uint64(5000)
	nonce := uint64(12)
	txindex := uint64(0)

	dispatcher := &mockREST2EthDispatcher{}
	r, router, res, req := newTestREST2EthAndMsg(dispatcher, "", "contract-name", nil)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("ResolveContractAddress", "contract-name").Return("contract-address", nil)
	expectContractSuccess(t, mcr, "contract-address")
	mockRPC := r.rpc.(*ethmocks.RPCClient)
	mockRPC.On("CallContext", context.Background(), mock.Anything, "eth_getTransactionByHash", "0x9999").
		Run(func(args mock.Arguments) {
			result := args[1].(*eth.TxnInfo)
			*result = eth.TxnInfo{
				BlockNumber:      (*ethbinding.HexBigInt)(big.NewInt(15)),
				Gas:              (*ethbinding.HexUint64)(&gas),
				GasPrice:         (*ethbinding.HexBigInt)(big.NewInt(0)),
				Nonce:            (*ethbinding.HexUint64)(&nonce),
				TransactionIndex: (*ethbinding.HexUint64)(&txindex),
				Value:            (*ethbinding.HexBigInt)(big.NewInt(10)),
				Input: &ethbinding.HexBytes{
					0x09, 0x23, 0xf7, 0x0f,
					0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
					0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
				},
			}
		}).
		Return(nil)

	req.Header.Set("X-Firefly-Transaction", "0x9999")
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	var resultBody map[string]interface{}
	err := json.NewDecoder(res.Result().Body).Decode(&resultBody)
	assert.NoError(err)
	assert.Equal(map[string]interface{}{"i": "0", "s": ""}, resultBody["inputArgs"])
}

func TestGetTransactionInfoFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, router, res, req := newTestREST2EthAndMsg(dispatcher, "", "contract-name", nil)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("ResolveContractAddress", "contract-name").Return("contract-address", nil)
	expectContractSuccess(t, mcr, "contract-address")
	mockRPC := r.rpc.(*ethmocks.RPCClient)
	mockRPC.On("CallContext", context.Background(), mock.Anything, "eth_getTransactionByHash", "0x9999").
		Return(fmt.Errorf("pop"))

	req.Header.Set("X-Firefly-Transaction", "0x9999")
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
}

func TestGetTransactionInfoDecodeFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	dispatcher := &mockREST2EthDispatcher{}
	r, router, res, req := newTestREST2EthAndMsg(dispatcher, "", "contract-name", nil)
	mcr := r.cr.(*contractregistrymocks.ContractStore)
	mcr.On("ResolveContractAddress", "contract-name").Return("contract-address", nil)
	expectContractSuccess(t, mcr, "contract-address")
	mockRPC := r.rpc.(*ethmocks.RPCClient)
	mockRPC.On("CallContext", context.Background(), mock.Anything, "eth_getTransactionByHash", "0x9999").
		Run(func(args mock.Arguments) {
			result := args[1].(*eth.TxnInfo)
			*result = eth.TxnInfo{
				Input: &ethbinding.HexBytes{},
			}
		}).
		Return(nil)

	req.Header.Set("X-Firefly-Transaction", "0x9999")
	router.ServeHTTP(res, req)

	assert.Equal(500, res.Result().StatusCode)
}
