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
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"testing"
	"time"

	"github.com/go-openapi/spec"
	"github.com/hyperledger/firefly-ethconnect/internal/auth"
	"github.com/hyperledger/firefly-ethconnect/internal/auth/authtest"
	"github.com/hyperledger/firefly-ethconnect/internal/contractregistry"
	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/ethbind"
	"github.com/hyperledger/firefly-ethconnect/internal/events"
	"github.com/hyperledger/firefly-ethconnect/internal/messages"
	"github.com/hyperledger/firefly-ethconnect/internal/tx"
	"github.com/hyperledger/firefly-ethconnect/mocks/contractregistrymocks"
	"github.com/julienschmidt/httprouter"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

type mockWebSocketServer struct {
	testChan chan interface{}
}

func (m *mockWebSocketServer) GetChannels(topic string) (chan<- interface{}, chan<- interface{}, <-chan error) {
	return nil, nil, nil
}

func (m *mockWebSocketServer) SendReply(message interface{}) {
	m.testChan <- message
}

type SolcJson struct {
	ABI string `json:"abi"`
	Bin string `json:"bin"`
}

var simpleEventsSol string

func simpleEventsSource() string {
	if simpleEventsSol == "" {
		simpleEventsBytes, _ := ioutil.ReadFile("../../test/simpleevents.sol")
		simpleEventsSol = string(simpleEventsBytes)
	}
	return simpleEventsSol
}

func TestCobraInitContractGateway(t *testing.T) {
	assert := assert.New(t)
	cmd := cobra.Command{}
	conf := &SmartContractGatewayConf{}
	CobraInitContractGateway(&cmd, conf)
	assert.NotNil(cmd.Flag("openapi-path"))
	assert.NotNil(cmd.Flag("openapi-baseurl"))
}

func TestNewSmartContractGatewayBadURL(t *testing.T) {
	NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: " :",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil, nil,
	)
}

func TestNewSmartContractGatewayWithEvents(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)
	assert := assert.New(t)
	s, err := NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
			SubscriptionManagerConf: events.SubscriptionManagerConf{
				EventLevelDBPath: path.Join(dir, "db"),
			},
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil, nil,
	)
	assert.NoError(err)
	assert.NotNil(s.(*smartContractGW).sm)
}

func TestNewSmartContractGatewayWithEventsFail(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)
	assert := assert.New(t)
	dbpath := path.Join(dir, "db")
	ioutil.WriteFile(dbpath, []byte("not a database"), 0644)
	_, err := NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
			SubscriptionManagerConf: events.SubscriptionManagerConf{
				EventLevelDBPath: dbpath,
			},
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil, nil,
	)
	assert.Regexp("Event-stream subscription manager", err.Error())
}

func TestPreDeployCompileAndPostDeploy(t *testing.T) {
	// writes real files and tests end to end
	assert := assert.New(t)
	msg := messages.DeployContract{
		Solidity:   simpleEventsSource(),
		RegisterAs: "Test 1",
	}
	msg.Headers.ID = "message1"
	dir := tempdir()
	defer cleanup(dir)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil, nil,
	)

	err := scgw.PreDeploy(&msg)
	assert.NoError(err)

	id := msg.Headers.ID
	assert.Equal("message1", id)
	assert.Empty(msg.Solidity)
	assert.NotEmpty(msg.Compiled)
	assert.NotEmpty(msg.DevDoc)

	deployStashBytes, err := ioutil.ReadFile(path.Join(dir, "abi_message1.deploy.json"))
	assert.NoError(err)
	var deployStash messages.DeployContract
	err = json.Unmarshal(deployStashBytes, &deployStash)
	assert.NoError(err)
	assert.NotEmpty(deployStash.CompilerVersion)

	contractAddr := ethbind.API.HexToAddress("0x0123456789AbcdeF0123456789abCdef01234567")
	receipt := messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				CommonHeaders: messages.CommonHeaders{
					ID:      "message2",
					MsgType: messages.MsgTypeTransactionSuccess,
				},
				ReqID:    "message1",
				ReqABIID: "message1",
			},
		},
		ContractAddress: &contractAddr,
		RegisterAs:      msg.RegisterAs,
	}
	err = scgw.PostDeploy(&receipt)
	assert.NoError(err)

	info, err := scgw.(*smartContractGW).cs.GetContractByAddress("0123456789abcdef0123456789abcdef01234567")
	assert.NoError(err)
	assert.NotEmpty(info)
	abiInfo, err := scgw.(*smartContractGW).cs.GetLocalABIInfo(info.ABI)
	assert.NoError(err)
	assert.NotNil(abiInfo)
	deployMsg, err := scgw.(*smartContractGW).cs.GetABI(contractregistry.ABILocation{
		ABIType: contractregistry.LocalABI,
		Name:    info.ABI,
	}, false)
	assert.NoError(err)
	runtimeABI, err := ethbind.API.ABIMarshalingToABIRuntime(deployMsg.Contract.ABI)
	assert.NoError(err)
	assert.Equal("set", runtimeABI.Methods["set"].Name)

	// Check we can list contracts back over REST
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	req := httptest.NewRequest("GET", "/contracts", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	var body []*contractregistry.ContractInfo
	err = json.NewDecoder(res.Body).Decode(&body)
	assert.NoError(err)
	assert.Equal(1, len(body))
	assert.Equal("0123456789abcdef0123456789abcdef01234567", body[0].Address)

	// Check we can get it back over REST
	req = httptest.NewRequest("GET", "/contracts/0x0123456789abcdef0123456789abcdef01234567", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	err = json.NewDecoder(res.Body).Decode(&info)
	assert.NoError(err)
	assert.Equal("0123456789abcdef0123456789abcdef01234567", info.Address)

	// Check we can list ABIs back over REST
	req = httptest.NewRequest("GET", "/abis", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	var abis []*contractregistry.ABIInfo
	err = json.NewDecoder(res.Body).Decode(&abis)
	assert.NoError(err)
	assert.Equal(1, len(abis))
	assert.Equal("message1", abis[0].ID)

	// Check we can get the full ABI swagger back over REST
	req = httptest.NewRequest("GET", "/abis/message1?swagger&noauth&schemes=http,https,wss", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	var swagger spec.Swagger
	err = json.NewDecoder(res.Body).Decode(&swagger)
	assert.NoError(err)
	assert.Equal("SimpleEvents", swagger.Info.Title)
	assert.Equal([]string{"http", "https"}, swagger.Schemes)
	assert.Nil(swagger.SecurityDefinitions)

	// Check we can get the full ABI back over REST
	req = httptest.NewRequest("GET", "/abis/message1?abi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	var abi ethbinding.RuntimeABI
	err = json.NewDecoder(res.Body).Decode(&abi)
	assert.NoError(err)
	assert.Equal("set", abi.Methods["set"].Name)

	// Check we can get the full contract swagger back over REST with contract addr that includes 0x prefix
	req = httptest.NewRequest("GET", "/contracts/0x0123456789abcdef0123456789abcdef01234567?swagger", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	err = json.NewDecoder(res.Body).Decode(&swagger)
	assert.NoError(err)
	assert.Equal("SimpleEvents", swagger.Info.Title)

	// Check we can get the full contract swagger back over REST
	req = httptest.NewRequest("GET", "/contracts/0123456789abcdef0123456789abcdef01234567?swagger", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	err = json.NewDecoder(res.Body).Decode(&swagger)
	assert.NoError(err)
	assert.Equal("SimpleEvents", swagger.Info.Title)

	// Check we can get the full contract ABI back over REST
	req = httptest.NewRequest("GET", "/contracts/0123456789abcdef0123456789abcdef01234567?abi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	err = json.NewDecoder(res.Body).Decode(&abi)
	assert.NoError(err)
	assert.Equal("set", abi.Methods["set"].Name)

	// Check we can get the full swagger back over REST using the registered name
	req = httptest.NewRequest("GET", "/contracts/Test+1?ui&from=0x0123456789abcdef0123456789abcdef01234567", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)

	// Check we can get the full swagger back over REST for download with a default from
	req = httptest.NewRequest("GET", "/contracts/0123456789abcdef0123456789abcdef01234567?swagger&from=0x0123456789abcdef0123456789abcdef01234567&download", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	err = json.NewDecoder(res.Body).Decode(&swagger)
	assert.NoError(err)
	assert.Equal("SimpleEvents", swagger.Info.Title)
	assert.Equal("attachment; filename=\"0123456789abcdef0123456789abcdef01234567.swagger.json\"", res.HeaderMap.Get("Content-Disposition"))
	assert.Equal("0x0123456789abcdef0123456789abcdef01234567", swagger.Parameters["fromParam"].SimpleSchema.Default)
	assert.Equal("/api/v1/contracts/0123456789abcdef0123456789abcdef01234567", swagger.BasePath)
}

func TestRegisterExistingContract(t *testing.T) {
	// writes real files and tests end to end
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil, nil,
	)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("files", "SimpleEvents.sol")
	part.Write([]byte(simpleEventsSource()))
	writer.Close()

	req := httptest.NewRequest("POST", "/abis", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Code)

	var abi contractregistry.ABIInfo
	json.NewDecoder(res.Body).Decode(&abi)
	assert.NotEmpty(abi.ID)

	req = httptest.NewRequest("POST", "/abis/"+abi.ID+"/0x0123456789abcdef0123456789abcdef01234567?fly-register=testcontract", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	var contract contractregistry.ContractInfo
	json.NewDecoder(res.Body).Decode(&contract)
	assert.Equal(201, res.Code)
	assert.Equal("/contracts/testcontract", contract.Path)

	req = httptest.NewRequest("POST", "/abis/"+abi.ID+"/0x0123456789abcdef0123456789abcdef01234567?fly-register=testcontract", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	var errBody map[string]interface{}
	json.NewDecoder(res.Body).Decode(&errBody)
	assert.Equal(409, res.Code)
	assert.Equal("Contract address 0123456789abcdef0123456789abcdef01234567 is already registered for name 'testcontract'", errBody["error"])
	assert.Equal(errors.RESTGatewayFriendlyNameClash.Code(), errBody["code"])

	req = httptest.NewRequest("GET", "/contracts/testcontract?swagger", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	returnedSwagger := spec.Swagger{}
	assert.Equal(200, res.Code)
	json.NewDecoder(res.Body).Decode(&returnedSwagger)
	assert.Equal("testcontract", returnedSwagger.Info.Extensions["x-firefly-registered-name"])
	assert.Equal("/api/v1/contracts/testcontract", returnedSwagger.BasePath)

}

func TestRemoteRegistrySwaggerOrABI(t *testing.T) {
	assert := assert.New(t)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	iMsg := newTestDeployMsg(t, "0123456789abcdef0123456789abcdef01234567")
	iMsg.Contract.Headers.ID = "xyz12345"
	mcs := &contractregistrymocks.ContractStore{}
	scgw.(*smartContractGW).cs = mcs

	router := &httprouter.Router{}
	scgw.AddRoutes(router)

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteGateway,
		Name:    "test",
	}, false).Return(iMsg, nil).Once()
	req := httptest.NewRequest("GET", "/g/test?swagger", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	returnedSwagger := spec.Swagger{}
	assert.Equal(200, res.Code)
	json.NewDecoder(res.Body).Decode(&returnedSwagger)
	assert.Equal("/api/v1/g/test", returnedSwagger.BasePath)

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteGateway,
		Name:    "test",
	}, false).Return(iMsg, nil).Once()
	req = httptest.NewRequest("GET", "/g/test?abi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	var returnedABI ethbinding.RuntimeABI
	assert.Equal(200, res.Code)
	json.NewDecoder(res.Body).Decode(&returnedABI)
	assert.Equal("set", returnedABI.Methods["set"].Name)

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteInstance,
		Name:    "test",
	}, true).Return(iMsg, nil).Once()
	req = httptest.NewRequest("GET", "/i/test?openapi&refresh", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	returnedSwagger = spec.Swagger{}
	assert.Equal(200, res.Code)
	json.NewDecoder(res.Body).Decode(&returnedSwagger)
	assert.Equal("/api/v1/i/test", returnedSwagger.BasePath)

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteInstance,
		Name:    "test",
	}, false).Return(iMsg, nil).Once()
	req = httptest.NewRequest("GET", "/instances/test", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Code)
	jsonRes := make(map[string]interface{})
	json.NewDecoder(res.Body).Decode(&jsonRes)
	assert.Equal("xyz12345", jsonRes["id"].(string))
	assert.Equal("0123456789abcdef0123456789abcdef01234567", jsonRes["address"].(string))

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteInstance,
		Name:    "test",
	}, false).Return(iMsg, nil).Once()
	req = httptest.NewRequest("GET", "/i/test?ui", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Code)
	html, _ := ioutil.ReadAll(res.Body)
	assert.Contains(string(html), "<html>")
	assert.Contains(string(html), "/instances/test?swagger")

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteGateway,
		Name:    "test",
	}, false).Return(iMsg, nil).Once()
	req = httptest.NewRequest("GET", "/g/test?ui&from=0x12345", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Code)
	html, _ = ioutil.ReadAll(res.Body)
	assert.Contains(string(html), "<html>")
	assert.Contains(string(html), "/gateways/test?swagger")
	assert.Contains(string(html), "0x12345")

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteGateway,
		Name:    "test",
	}, false).Return(iMsg, nil).Once()
	req = httptest.NewRequest("GET", "/g/test?ui&from=0x12345&factory", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Code)
	html, _ = ioutil.ReadAll(res.Body)
	assert.Contains(string(html), "<html>")
	assert.Contains(string(html), "Factory API")

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteInstance,
		Name:    "test",
	}, false).Return(nil, nil).Once()
	req = httptest.NewRequest("GET", "/instances/test?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Code)

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteGateway,
		Name:    "test",
	}, false).Return(nil, nil).Once()
	req = httptest.NewRequest("GET", "/gateways/test?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Code)

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteInstance,
		Name:    "test",
	}, false).Return(nil, fmt.Errorf("pop")).Once()
	req = httptest.NewRequest("GET", "/instances/test?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(500, res.Code)

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteGateway,
		Name:    "test",
	}, false).Return(nil, fmt.Errorf("pop")).Once()
	req = httptest.NewRequest("GET", "/gateways/test?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(500, res.Code)

	mcs.On("Close").Return()
	scgw.Shutdown()
	mcs.AssertExpectations(t)
}

func TestRemoteRegistryBadABI(t *testing.T) {
	assert := assert.New(t)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	iMsg := newTestDeployMsg(t, "0123456789abcdef0123456789abcdef01234567")
	iMsg.Contract.Headers.ID = "xyz12345"
	// Append two fallback methods - that is invalid
	iMsg.Contract.ABI = append(iMsg.Contract.ABI, ethbinding.ABIElementMarshaling{
		Type: "fallback",
	})
	iMsg.Contract.ABI = append(iMsg.Contract.ABI, ethbinding.ABIElementMarshaling{
		Type: "fallback",
	})
	mcs := &contractregistrymocks.ContractStore{}
	scgw.(*smartContractGW).cs = mcs

	router := &httprouter.Router{}
	scgw.AddRoutes(router)

	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.RemoteGateway,
		Name:    "test",
	}, false).Return(iMsg, nil)
	req := httptest.NewRequest("GET", "/g/test?swagger", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	var msg map[string]interface{}
	assert.Equal(400, res.Code)
	json.NewDecoder(res.Body).Decode(&msg)
	assert.Regexp("Invalid ABI", msg["error"])

	mcs.On("Close").Return()
	scgw.Shutdown()
	mcs.AssertExpectations(t)
}

func TestRegisterContractBadAddress(t *testing.T) {
	// writes real files and tests end to end
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	router := &httprouter.Router{}
	scgw.AddRoutes(router)

	req := httptest.NewRequest("POST", "/abis/ID/badness", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Code)
	var resBody map[string]interface{}
	json.NewDecoder(res.Body).Decode(&resBody)
	assert.Equal("Invalid address in path - must be a 40 character hex string with optional 0x prefix", resBody["error"])
}

func TestRegisterContractNoRegisteredName(t *testing.T) {
	// writes real files and tests end to end
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	router := &httprouter.Router{}
	scgw.AddRoutes(router)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("files", "SimpleEvents.sol")
	part.Write([]byte(simpleEventsSource()))
	writer.Close()

	req := httptest.NewRequest("POST", "/abis", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)
	assert.Equal(200, res.Code)

	var abi contractregistry.ABIInfo
	json.NewDecoder(res.Body).Decode(&abi)
	assert.NotEmpty(abi.ID)

	req = httptest.NewRequest("POST", "/abis/"+abi.ID+"/0x0123456789abcdef0123456789abcdef01234567", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	var contract contractregistry.ContractInfo
	json.NewDecoder(res.Body).Decode(&contract)
	assert.Equal(201, res.Code)
	assert.Equal("/contracts/0123456789abcdef0123456789abcdef01234567", contract.Path)
}

func TestRegisterContractBadABI(t *testing.T) {
	// writes real files and tests end to end
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	router := &httprouter.Router{}
	scgw.AddRoutes(router)

	req := httptest.NewRequest("POST", "/abis/BADID/0x0123456789abcdef0123456789abcdef01234567", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Code)
	var resBody map[string]interface{}
	json.NewDecoder(res.Body).Decode(&resBody)
	assert.Equal("No ABI found with ID BADID", resBody["error"])
}

func TestPreDeployCompileFailure(t *testing.T) {
	assert := assert.New(t)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: "/anypath",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	msg := &messages.DeployContract{
		Solidity: "bad solidity",
	}
	err := scgw.PreDeploy(msg)
	assert.Regexp("Solidity compilation failed", err.Error())
}

func TestPreDeployMsgWrite(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: path.Join(dir, "badpath"),
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	msg := &messages.DeployContract{
		Solidity: simpleEventsSource(),
	}

	err := scgw.writeAbiInfo("request1", msg)
	assert.Regexp("Failed to write deployment details", err.Error())
}

func TestPostDeployNoRegisteredName(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	contractAddr := ethbind.API.HexToAddress("0x0123456789AbcdeF0123456789abCdef01234567")
	scgw := s.(*smartContractGW)
	replyMsg := &messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				CommonHeaders: messages.CommonHeaders{
					MsgType: messages.MsgTypeTransactionSuccess,
				},
				ReqID: "message1",
			},
		},
		ContractAddress: &contractAddr,
	}

	deployFile := path.Join(dir, "abi_message1.deploy.json")
	deployMsg := &messages.DeployContract{}
	deployBytes, _ := json.Marshal(deployMsg)
	ioutil.WriteFile(deployFile, deployBytes, 0644)
	err := scgw.PostDeploy(replyMsg)
	assert.NoError(err)

	contractInfo, err := scgw.cs.GetContractByAddress("0123456789abcdef0123456789abcdef01234567")
	assert.NoError(err)
	assert.Equal("", contractInfo.RegisteredAs)
	assert.Equal("/contracts/0123456789abcdef0123456789abcdef01234567", contractInfo.Path)
}

func TestPostDeployRemoteRegisteredName(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	mcs := &contractregistrymocks.ContractStore{}
	s.(*smartContractGW).cs = mcs

	contractAddr := ethbind.API.HexToAddress("0x0123456789AbcdeF0123456789abCdef01234567")
	scgw := s.(*smartContractGW)
	replyMsg := &messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				CommonHeaders: messages.CommonHeaders{
					Context: map[string]interface{}{
						contractregistry.RemoteRegistryContextKey: true,
					},
					MsgType: messages.MsgTypeTransactionSuccess,
				},
				ReqID: "message1",
			},
		},
		ContractAddress: &contractAddr,
		RegisterAs:      "lobster",
	}

	mcs.On("AddRemoteInstance", "lobster", "0x0123456789abcdef0123456789abcdef01234567").Return(nil)

	deployFile := path.Join(dir, "abi_message1.deploy.json")
	deployMsg := &messages.DeployContract{}
	deployBytes, _ := json.Marshal(deployMsg)
	ioutil.WriteFile(deployFile, deployBytes, 0644)
	err := scgw.PostDeploy(replyMsg)
	assert.NoError(err)

	assert.Equal("http://localhost/api/v1/instances/lobster?openapi", replyMsg.ContractSwagger)

	mcs.AssertExpectations(t)
}

func TestPostDeployRemoteRegisteredNameNotSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	mcs := &contractregistrymocks.ContractStore{}
	s.(*smartContractGW).cs = mcs

	contractAddr := ethbind.API.HexToAddress("0x0123456789AbcdeF0123456789abCdef01234567")
	scgw := s.(*smartContractGW)
	replyMsg := &messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				CommonHeaders: messages.CommonHeaders{
					Context: map[string]interface{}{
						contractregistry.RemoteRegistryContextKey: true,
					},
					MsgType: messages.MsgTypeTransactionFailure,
				},
				ReqID: "message1",
			},
		},
		ContractAddress: &contractAddr,
		RegisterAs:      "lobster",
	}

	deployFile := path.Join(dir, "abi_message1.deploy.json")
	deployMsg := &messages.DeployContract{}
	deployBytes, _ := json.Marshal(deployMsg)
	ioutil.WriteFile(deployFile, deployBytes, 0644)
	err := scgw.PostDeploy(replyMsg)
	assert.NoError(err)

	assert.Empty(replyMsg.ContractSwagger)

	mcs.AssertExpectations(t)
}

func TestPostDeployMissingContractAddress(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	replyMsg := &messages.TransactionReceipt{
		ReplyCommon: messages.ReplyCommon{
			Headers: messages.ReplyHeaders{
				ReqID: "message1",
			},
		},
	}
	ioutil.WriteFile(path.Join(dir, "message1.deploy.json"), []byte("invalid json"), 0664)

	err := scgw.PostDeploy(replyMsg)
	assert.Regexp("message1: Missing contract address in receipt", err.Error())
}

func TestGetContractOrABIFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil, nil,
	)
	mcs := &contractregistrymocks.ContractStore{}
	scgw := s.(*smartContractGW)
	scgw.cs = mcs

	// Contract that does not exist in the index
	mcs.On("GetContractByAddress", "nonexistent").Return(nil, fmt.Errorf("pop")).Once()
	mcs.On("ResolveContractAddress", "nonexistent").Return("", fmt.Errorf("pop")).Once()
	req := httptest.NewRequest("GET", "/contracts/nonexistent?openapi", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Result().StatusCode)

	// ABI that does not exist in the index
	mcs.On("GetLocalABIInfo", "23456789abcdef0123456789abcdef0123456789").Return(nil, fmt.Errorf("pop")).Once()
	req = httptest.NewRequest("GET", "/abis/23456789abcdef0123456789abcdef0123456789?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router = &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Result().StatusCode)

	mcs.AssertExpectations(t)
}

func TestGetContractUI(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil, nil,
	)
	mcs := &contractregistrymocks.ContractStore{}
	scgw := s.(*smartContractGW)
	scgw.cs = mcs

	mcs.On("GetContractByAddress", "123456789abcdef0123456789abcdef012345678").Return(&contractregistry.ContractInfo{
		ABI:     "abi1",
		Address: "123456789abcdef0123456789abcdef012345678",
	}, nil)
	mcs.On("GetABI", contractregistry.ABILocation{
		ABIType: contractregistry.LocalABI,
		Name:    "abi1",
	}, false).Return(&contractregistry.DeployContractWithAddress{}, nil)

	req := httptest.NewRequest("GET", "/contracts/123456789abcdef0123456789abcdef012345678?ui", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	body, _ := ioutil.ReadAll(res.Body)
	assert.Regexp("Ethconnect REST Gateway", string(body))

	mcs.AssertExpectations(t)
}

func TestAddABISingleSolidity(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("files", "SimpleEvents.sol")
	part.Write([]byte(simpleEventsSource()))
	writer.Close()

	req := httptest.NewRequest("POST", "/abis", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	info := &contractregistry.ABIInfo{}
	err := json.NewDecoder(res.Body).Decode(info)
	assert.NoError(err)
	assert.Equal("SimpleEvents", info.Name)
}

func TestAddABISingleSolidityBadContractName(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("files", "SimpleEvents.sol")
	part.Write([]byte(simpleEventsSource()))
	writer.Close()

	req := httptest.NewRequest("POST", "/abis?contract=badness", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
}
func TestAddABIZipNested(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("files", "solfiles.zip")
	zipWriter := zip.NewWriter(part)
	solWriter, _ := zipWriter.Create("solfiles/SimpleEvents.sol")
	solWriter.Write([]byte(simpleEventsSource()))
	zipWriter.Close()
	writer.WriteField("source", "solfiles/SimpleEvents.sol")
	writer.Close()

	req := httptest.NewRequest("POST", "/abis", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	info := &contractregistry.ABIInfo{}
	err := json.NewDecoder(res.Body).Decode(info)
	assert.NoError(err)
	assert.Equal("SimpleEvents", info.Name)
}

func TestAddABIZipNestedListSolidity(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("files", "solfiles.zip")
	zipWriter := zip.NewWriter(part)
	solWriter, _ := zipWriter.Create("solfiles/SimpleEvents.sol")
	solWriter.Write([]byte(simpleEventsSource()))
	zipWriter.Close()
	writer.WriteField("source", "solfiles/SimpleEvents.sol")
	writer.Close()

	req := httptest.NewRequest("POST", "/abis?findsolidity", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	var solFiles []string
	err := json.NewDecoder(res.Body).Decode(&solFiles)
	assert.NoError(err)
	assert.Equal(1, len(solFiles))
	assert.Equal("solfiles/SimpleEvents.sol", solFiles[0])
}

func TestAddABIZipNestedListContracts(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("files", "solfiles.zip")
	zipWriter := zip.NewWriter(part)
	solWriter, _ := zipWriter.Create("solfiles/SimpleEvents.sol")
	solWriter.Write([]byte(simpleEventsSource()))
	zipWriter.Close()
	writer.WriteField("source", "solfiles/SimpleEvents.sol")
	writer.Close()

	req := httptest.NewRequest("POST", "/abis?findcontracts", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)

	assert.Equal(200, res.Result().StatusCode)
	var solFiles []string
	err := json.NewDecoder(res.Body).Decode(&solFiles)
	assert.NoError(err)
	assert.Equal(1, len(solFiles))
	assert.Equal("solfiles/SimpleEvents.sol:SimpleEvents", solFiles[0])
}

func TestAddABIBadZip(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("files", "SimpleEvents.zip")
	part.Write([]byte(simpleEventsSource()))
	writer.Close()

	req := httptest.NewRequest("POST", "/abis", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	errInfo := &errors.RESTError{}
	err := json.NewDecoder(res.Body).Decode(errInfo)
	assert.NoError(err)
	assert.Regexp("Error unarchiving supplied zip file", errInfo.Message)
	assert.Equal(errors.RESTGatewayCompileContractUnzip.Code(), errInfo.Code)
}

func TestAddABIZipNestedNoSource(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("files", "solfiles.zip")
	zipWriter := zip.NewWriter(part)
	solWriter, _ := zipWriter.Create("solfiles/SimpleEvents.sol")
	solWriter.Write([]byte(simpleEventsSource()))
	zipWriter.Close()
	writer.Close()

	req := httptest.NewRequest("POST", "/abis", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	errInfo := &errors.RESTError{}
	err := json.NewDecoder(res.Body).Decode(errInfo)
	assert.NoError(err)
	assert.Regexp("Failed to compile solidity.*No .sol files found in root. Please set a 'source' query param or form field to the relative path of your solidity", errInfo.Message)
	assert.Equal(errors.RESTGatewayCompileContractCompileFailed.Code(), errInfo.Code)
}

func TestAddABIZiNotMultipart(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	req := httptest.NewRequest("POST", "/abis", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	errInfo := &errors.RESTError{}
	err := json.NewDecoder(res.Body).Decode(errInfo)
	assert.NoError(err)
	assert.Regexp(errInfo.Message, "Could not parse supplied multi-part form data: request Content-Type isn't multipart/form-data")
	assert.Equal(errors.RESTGatewayCompileContractInvalidFormData.Code(), errInfo.Code)
}

func TestCompileMultipartFormSolidityBadDir(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	_, err := scgw.compileMultipartFormSolidity(path.Join(dir, "baddir"), nil)
	assert.Regexp("Failed to read extracted multi-part form data", err)
}

func TestCompileMultipartFormSolidityBadSolc(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	os.Setenv("FLY_SOLC_0_99", "badness")

	ioutil.WriteFile(path.Join(dir, "solidity.sol"), []byte(simpleEventsSource()), 0644)
	req := httptest.NewRequest("POST", "/abis?compiler=0.99", bytes.NewReader([]byte{}))
	_, err := scgw.compileMultipartFormSolidity(dir, req)
	assert.Regexp("Failed checking solc version", err.Error())
	os.Unsetenv("FLY_SOLC_0_99")
}

func TestCompileMultipartFormSolidityBadCompilerVerReq(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	ioutil.WriteFile(path.Join(dir, "solidity.sol"), []byte(simpleEventsSource()), 0644)
	req := httptest.NewRequest("POST", "/abis?compiler=0.99", bytes.NewReader([]byte{}))
	_, err := scgw.compileMultipartFormSolidity(dir, req)
	assert.Regexp("Failed checking solc version.*Could not find a configured compiler for requested Solidity major version 0.99", err)
}

func TestCompileMultipartFormSolidityBadSolidity(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	ioutil.WriteFile(path.Join(dir, "solidity.sol"), []byte("this is not the solidity you are looking for"), 0644)
	req := httptest.NewRequest("POST", "/abis", bytes.NewReader([]byte{}))
	_, err := scgw.compileMultipartFormSolidity(dir, req)
	assert.Regexp("Failed to compile", err.Error())
}

func TestExtractMultiPartFileBadFile(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	err := scgw.extractMultiPartFile(dir, &multipart.FileHeader{
		Filename: "/stuff.zip",
	})
	assert.Regexp("Filenames cannot contain slashes. Use a zip file to upload a directory structure", err)
}

func TestExtractMultiPartFileBadInput(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	err := scgw.extractMultiPartFile(dir, &multipart.FileHeader{
		Filename: "stuff.zip",
	})
	assert.Regexp("Failed to read archive", err)
}

func TestStoreDeployableABIMissingABI(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	_, err := scgw.storeDeployableABI(&messages.DeployContract{}, nil)
	assert.Regexp("Must supply ABI to install an existing ABI into the REST Gateway", err)
}

func testGWPath(method, path string, results interface{}, sm *mockSubMgr) (res *httptest.ResponseRecorder) {
	return testGWPathBody(method, path, results, sm, nil)
}

func testGWPathBody(method, path string, results interface{}, sm *mockSubMgr, body io.Reader) (res *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, body)
	res = httptest.NewRecorder()
	s := &smartContractGW{}
	if sm != nil {
		s.sm = sm
	}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	json.NewDecoder(res.Body).Decode(&results)
	return
}

func TestAddStreamNoSubMgr(t *testing.T) {
	assert := assert.New(t)
	res := testGWPath("POST", events.StreamPathPrefix, nil, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestAddStreamOK(t *testing.T) {
	assert := assert.New(t)
	spec := &events.StreamInfo{Type: "webhook", Name: "stream-1", Timestamps: true}
	b, _ := json.Marshal(spec)
	req := httptest.NewRequest("POST", events.StreamPathPrefix, bytes.NewReader(b))
	res := httptest.NewRecorder()
	s := &smartContractGW{}
	s.sm = &mockSubMgr{}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	var newSpec events.StreamInfo
	json.NewDecoder(res.Body).Decode(&newSpec)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("webhook", newSpec.Type)
	assert.Equal("stream-1", newSpec.Name)
	assert.Equal(true, newSpec.Timestamps)
	s.Shutdown()
}

func TestAddStreamDefaultNoTimestamps(t *testing.T) {
	assert := assert.New(t)
	spec := &events.StreamInfo{Type: "webhook", Name: "stream-no-timestamps"}
	b, _ := json.Marshal(spec)
	req := httptest.NewRequest("POST", events.StreamPathPrefix, bytes.NewReader(b))
	res := httptest.NewRecorder()
	s := &smartContractGW{}
	s.sm = &mockSubMgr{}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	var newSpec events.StreamInfo
	json.NewDecoder(res.Body).Decode(&newSpec)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("webhook", newSpec.Type)
	assert.Equal("stream-no-timestamps", newSpec.Name)
	assert.Equal(false, newSpec.Timestamps)
	s.Shutdown()
}

func TestAddStreamBadData(t *testing.T) {
	assert := assert.New(t)
	req := httptest.NewRequest("POST", events.StreamPathPrefix, bytes.NewReader([]byte(":bad json")))
	res := httptest.NewRecorder()
	s := &smartContractGW{}
	s.sm = &mockSubMgr{}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	var resError errors.RESTError
	json.NewDecoder(res.Body).Decode(&resError)
	assert.Equal(400, res.Result().StatusCode)
	assert.Regexp("Invalid event stream specification", resError.Message)
}

func TestAddStreamSubMgrError(t *testing.T) {
	assert := assert.New(t)
	spec := &events.StreamInfo{Type: "webhook"}
	b, _ := json.Marshal(spec)
	req := httptest.NewRequest("POST", events.StreamPathPrefix, bytes.NewReader(b))
	res := httptest.NewRecorder()
	s := &smartContractGW{}
	s.sm = &mockSubMgr{err: fmt.Errorf("pop")}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	var resError errors.RESTError
	json.NewDecoder(res.Body).Decode(&resError)
	assert.Equal(400, res.Result().StatusCode)
	assert.Regexp("pop", resError.Message)
}

func TestUpdateStreamNoSubMgr(t *testing.T) {
	assert := assert.New(t)
	res := testGWPath("PATCH", events.StreamPathPrefix+"/123", nil, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestUpdateStreamOK(t *testing.T) {
	assert := assert.New(t)
	updatedSpec := &events.StreamInfo{Type: "webhook", Name: "stream-new-name", ID: "123", Timestamps: true}
	modSpec := &events.StreamInfo{Timestamps: true}
	b, _ := json.Marshal(modSpec)
	req := httptest.NewRequest("PATCH", events.StreamPathPrefix+"/123", bytes.NewReader(b))
	res := httptest.NewRecorder()
	s := &smartContractGW{}
	s.sm = &mockSubMgr{
		stream: updatedSpec,
	}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	var newSpec events.StreamInfo
	json.NewDecoder(res.Body).Decode(&newSpec)
	assert.Equal(true, newSpec.Timestamps)
	assert.Equal(200, res.Result().StatusCode)
	s.Shutdown()
}

func TestUpdateStreamBadData(t *testing.T) {
	assert := assert.New(t)
	req := httptest.NewRequest("PATCH", events.StreamPathPrefix+"/123", bytes.NewReader([]byte(":bad json")))
	res := httptest.NewRecorder()
	s := &smartContractGW{}
	s.sm = &mockSubMgr{}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	var resError errors.RESTError
	json.NewDecoder(res.Body).Decode(&resError)
	assert.Equal(400, res.Result().StatusCode)
	assert.Regexp("Invalid event stream specification", resError.Message)
}

func TestUpdateStreamNotFoundError(t *testing.T) {
	assert := assert.New(t)
	spec := &events.StreamInfo{Type: "webhook", ID: "123"}
	b, _ := json.Marshal(spec)
	req := httptest.NewRequest("PATCH", events.StreamPathPrefix+"/123", bytes.NewReader(b))
	res := httptest.NewRecorder()
	s := &smartContractGW{}
	s.sm = &mockSubMgr{err: fmt.Errorf("pop")}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	var resError errors.RESTError
	json.NewDecoder(res.Body).Decode(&resError)
	assert.Equal(404, res.Result().StatusCode)
	assert.Regexp("pop", resError.Message)
}

func TestUpdateStreamSubMgrError(t *testing.T) {
	assert := assert.New(t)
	spec := &events.StreamInfo{Type: "webhook", ID: "123"}
	updatedSpec := &events.StreamInfo{Timestamps: true}
	b, _ := json.Marshal(updatedSpec)
	req := httptest.NewRequest("PATCH", events.StreamPathPrefix+"/123", bytes.NewReader(b))
	res := httptest.NewRecorder()
	s := &smartContractGW{}
	s.sm = &mockSubMgr{
		stream:          spec,
		updateStreamErr: fmt.Errorf("pop"),
		err:             nil,
	}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	var resError errors.RESTError
	json.NewDecoder(res.Body).Decode(&resError)
	assert.Equal(500, res.Result().StatusCode)
	assert.Regexp("pop", resError.Message)
}

func TestListStreamsNoSubMgr(t *testing.T) {
	assert := assert.New(t)
	res := testGWPath("GET", events.StreamPathPrefix, nil, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestListStreams(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{
		streams: []*events.StreamInfo{
			{
				TimeSorted: messages.TimeSorted{
					CreatedISO8601: time.Now().UTC().Format(time.RFC3339),
				}, ID: "earlier", Name: "stream-1",
			},
			{
				TimeSorted: messages.TimeSorted{
					CreatedISO8601: time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339),
				}, ID: "later", Name: "stream-2",
			},
		},
	}
	var results []*events.StreamInfo
	res := testGWPath("GET", events.StreamPathPrefix, &results, mockSubMgr)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(2, len(results))
	assert.Equal("later", results[0].ID)
	assert.Equal("stream-2", results[0].Name)
	assert.Equal("earlier", results[1].ID)
	assert.Equal("stream-1", results[1].Name)
}

func TestListSubs(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{
		subs: []*events.SubscriptionInfo{
			{
				TimeSorted: messages.TimeSorted{
					CreatedISO8601: time.Now().UTC().Format(time.RFC3339),
				}, ID: "earlier",
			},
			{
				TimeSorted: messages.TimeSorted{
					CreatedISO8601: time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339),
				}, ID: "later",
			},
		},
	}
	var results []*events.SubscriptionInfo
	res := testGWPath("GET", events.SubPathPrefix, &results, mockSubMgr)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(2, len(results))
	assert.Equal("later", results[0].ID)
	assert.Equal("earlier", results[1].ID)
}

func TestGetSub(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{
		sub: &events.SubscriptionInfo{ID: "123"},
	}
	var result events.SubscriptionInfo
	res := testGWPath("GET", events.SubPathPrefix+"/123", &result, mockSubMgr)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("123", result.ID)
}

func TestGetStream(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{
		stream: &events.StreamInfo{ID: "123"},
	}
	var result events.StreamInfo
	res := testGWPath("GET", events.StreamPathPrefix+"/123", &result, mockSubMgr)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("123", result.ID)
}

func TestGetSubNoSubMgr(t *testing.T) {
	assert := assert.New(t)

	var result events.SubscriptionInfo
	res := testGWPath("GET", events.SubPathPrefix+"/123", &result, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestGetSubNotFound(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{err: fmt.Errorf("not found")}
	var result events.SubscriptionInfo
	res := testGWPath("GET", events.SubPathPrefix+"/123", &result, mockSubMgr)
	assert.Equal(404, res.Result().StatusCode)
}

func TestDeleteSub(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{}
	res := testGWPath("DELETE", events.SubPathPrefix+"/123", nil, mockSubMgr)
	assert.Equal(204, res.Result().StatusCode)
}

func TestResetSub(t *testing.T) {
	assert := assert.New(t)

	reqData := map[string]interface{}{
		"fromBlock": "0",
	}
	b, _ := json.Marshal(&reqData)
	mockSubMgr := &mockSubMgr{}
	res := testGWPathBody("POST", events.SubPathPrefix+"/123/reset", nil, mockSubMgr, bytes.NewReader(b))
	assert.Equal(204, res.Result().StatusCode)
}

func TestResetSubFail(t *testing.T) {
	assert := assert.New(t)

	reqData := map[string]interface{}{
		"fromBlock": "0",
	}
	b, _ := json.Marshal(&reqData)
	mockSubMgr := &mockSubMgr{
		err: fmt.Errorf("pop"),
	}
	res := testGWPathBody("POST", events.SubPathPrefix+"/123/reset", nil, mockSubMgr, bytes.NewReader(b))
	assert.Equal(500, res.Result().StatusCode)
}

func TestResetSubNoManager(t *testing.T) {
	assert := assert.New(t)
	res := testGWPath("POST", events.SubPathPrefix+"/123/reset", nil, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestDeleteStream(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{}
	res := testGWPath("DELETE", events.StreamPathPrefix+"/123", nil, mockSubMgr)
	assert.Equal(204, res.Result().StatusCode)
}

func TestDeleteSubNoSubMgr(t *testing.T) {
	assert := assert.New(t)

	res := testGWPath("DELETE", events.SubPathPrefix+"/123", nil, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestDeleteSubError(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{err: fmt.Errorf("not found")}
	var errInfo = errors.RESTError{}
	res := testGWPath("DELETE", events.SubPathPrefix+"/123", &errInfo, mockSubMgr)
	assert.Equal(500, res.Result().StatusCode)
	assert.Equal("not found", errInfo.Message)
}

func TestSuspendStream(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{}
	res := testGWPath("POST", events.StreamPathPrefix+"/123/suspend", nil, mockSubMgr)
	assert.Equal(204, res.Result().StatusCode)
	assert.True(mockSubMgr.suspended)
}

func TestResumeStream(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{}
	res := testGWPath("POST", events.StreamPathPrefix+"/123/resume", nil, mockSubMgr)
	assert.Equal(204, res.Result().StatusCode)
	assert.True(mockSubMgr.resumed)
}

func TestResumeStreamFail(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{err: fmt.Errorf("pop")}
	var errInfo = errors.RESTError{}
	res := testGWPath("POST", events.StreamPathPrefix+"/123/resume", &errInfo, mockSubMgr)
	assert.Equal(500, res.Result().StatusCode)
	assert.Equal("pop", errInfo.Message)
}

func TestSuspendNoSubMgr(t *testing.T) {
	assert := assert.New(t)

	res := testGWPath("POST", events.StreamPathPrefix+"/123/resume", nil, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestWithEventsAuthRequiresAuth(t *testing.T) {
	assert := assert.New(t)

	auth.RegisterSecurityModule(&authtest.TestSecurityModule{})

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)

	router := &httprouter.Router{}

	router.GET("/", scgw.(*smartContractGW).withEventsAuth(
		func(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
			res.WriteHeader(200)
		}))

	req := httptest.NewRequest("GET", "/", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	assert.Equal(res.Code, 401)

	auth.RegisterSecurityModule(nil)
}

func TestSendReplyBroadcast(t *testing.T) {
	assert := assert.New(t)
	testMessage := "hello world"

	ws := &mockWebSocketServer{
		testChan: make(chan interface{}),
	}

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, ws,
	)

	go scgw.SendReply(testMessage)
	receivedReply := <-ws.testChan
	assert.Equal(testMessage, receivedReply)
}

func TestPublishBadABI(t *testing.T) {
	// writes real files and tests end to end
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	router := &httprouter.Router{}
	scgw.AddRoutes(router)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fw, _ := writer.CreateFormField("abi")
	io.Copy(fw, bytes.NewReader([]byte("blah blah blah bad abi")))
	writer.Close()
	req, _ := http.NewRequest("POST", "/abis", bytes.NewReader(body.Bytes()))
	req.Header.Add("Content-Type", writer.FormDataContentType())

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(400, res.Code)
	var resBody map[string]interface{}
	json.NewDecoder(res.Body).Decode(&resBody)
	assert.Equal("Could not parse supplied multi-part form data: invalid character 'b' looking for beginning of value", resBody["error"])
}

func TestPublishBadBytecode(t *testing.T) {
	// writes real files and tests end to end
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	router := &httprouter.Router{}
	scgw.AddRoutes(router)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fw, _ := writer.CreateFormField("bytecode")
	io.Copy(fw, bytes.NewReader([]byte("0xNOTHEX")))
	writer.Close()
	req, _ := http.NewRequest("POST", "/abis", bytes.NewReader(body.Bytes()))
	req.Header.Add("Content-Type", writer.FormDataContentType())

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(400, res.Code)
	var resBody map[string]interface{}
	json.NewDecoder(res.Body).Decode(&resBody)
	assert.Equal("Could not parse supplied multi-part form data: encoding/hex: invalid byte: U+004E 'N'", resBody["error"])
}

func TestPublishPreCompiled(t *testing.T) {
	// writes real files and tests end to end
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	router := &httprouter.Router{}
	scgw.AddRoutes(router)

	b, _ := ioutil.ReadFile(path.Join("..", "..", "test", "simpleevents.solc.output.json"))
	var contract SolcJson
	json.Unmarshal(b, &contract)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fw, _ := writer.CreateFormField("abi")
	io.Copy(fw, bytes.NewReader([]byte(contract.ABI)))
	fw, _ = writer.CreateFormField("bytecode")
	io.Copy(fw, bytes.NewReader([]byte(contract.Bin)))
	writer.Close()
	req, _ := http.NewRequest("POST", "/abis", bytes.NewReader(body.Bytes()))
	req.Header.Add("Content-Type", writer.FormDataContentType())

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Code)

	files, _ := ioutil.ReadDir(dir)

	deployedJson, err := ioutil.ReadFile(path.Join(dir, files[0].Name()))
	assert.NoError(err)
	var deployStash messages.DeployContract
	err = json.Unmarshal(deployedJson, &deployStash)
	assert.NoError(err)
	assert.NotEmpty(deployStash.ABI)
	assert.NotEmpty(deployStash.Compiled)
}

func TestResolveAddressFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)

	deployMsg, name, info, err := scgw.(*smartContractGW).resolveAddressOrName("test")
	assert.Regexp("No contract instance registered with address test", err)
	assert.Nil(deployMsg)
	assert.Nil(info)
	assert.Equal("", name)
}

func TestResolveAddressGetContractFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
			BaseURL:     "http://localhost/api/v1",
		},
		&tx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil, nil,
	)
	mcs := &contractregistrymocks.ContractStore{}
	scgw.(*smartContractGW).cs = mcs

	mcs.On("GetContractByAddress", "test").Return(nil, fmt.Errorf("bad address")).Once()
	mcs.On("ResolveContractAddress", "test").Return("address1", nil).Once()
	mcs.On("GetContractByAddress", "address1").Return(nil, fmt.Errorf("bad address")).Once()

	deployMsg, name, info, err := scgw.(*smartContractGW).resolveAddressOrName("test")
	assert.Regexp("bad address", err)
	assert.Nil(deployMsg)
	assert.Nil(info)
	assert.Equal("", name)
}
