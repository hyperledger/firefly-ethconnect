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
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"testing"
	"time"

	"github.com/kaleido-io/ethconnect/internal/kldauth"
	"github.com/kaleido-io/ethconnect/internal/kldauth/kldauthtest"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-openapi/spec"
	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldevents"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldtx"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
	)
}

func TestNewSmartContractGatewayWithEvents(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)
	assert := assert.New(t)
	s, err := NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
			SubscriptionManagerConf: kldevents.SubscriptionManagerConf{
				EventLevelDBPath: path.Join(dir, "db"),
			},
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
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
			SubscriptionManagerConf: kldevents.SubscriptionManagerConf{
				EventLevelDBPath: dbpath,
			},
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
	)
	assert.Regexp("Event-stream subscription manager", err.Error())
}

func TestPreDeployCompileAndPostDeploy(t *testing.T) {
	// writes real files and tests end to end
	assert := assert.New(t)
	msg := kldmessages.DeployContract{
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
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
	var deployStash kldmessages.DeployContract
	err = json.Unmarshal(deployStashBytes, &deployStash)
	assert.NoError(err)
	assert.NotEmpty(deployStash.CompilerVersion)

	contractAddr := common.HexToAddress("0x0123456789AbcdeF0123456789abCdef01234567")
	receipt := kldmessages.TransactionReceipt{
		ReplyCommon: kldmessages.ReplyCommon{
			Headers: kldmessages.ReplyHeaders{
				CommonHeaders: kldmessages.CommonHeaders{
					ID:      "message2",
					MsgType: kldmessages.MsgTypeTransactionSuccess,
				},
				ReqID: "message1",
			},
		},
		ContractAddress: &contractAddr,
		RegisterAs:      msg.RegisterAs,
	}
	err = scgw.PostDeploy(&receipt)
	assert.NoError(err)

	deployMsg, abiID, err := scgw.(*smartContractGW).loadDeployMsgForInstance("0123456789abcdef0123456789abcdef01234567")
	assert.NoError(err)
	assert.NotEmpty(abiID)
	assert.Equal("set", deployMsg.ABI.Methods["set"].Name)

	// Check we can list it back over REST
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	req := httptest.NewRequest("GET", "/contracts", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	var body []*contractInfo
	err = json.NewDecoder(res.Body).Decode(&body)
	assert.NoError(err)
	assert.Equal(1, len(body))
	assert.Equal("0123456789abcdef0123456789abcdef01234567", body[0].Address)

	// Check we can get it back over REST
	req = httptest.NewRequest("GET", "/contracts/0x0123456789abcdef0123456789abcdef01234567", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	var info contractInfo
	err = json.NewDecoder(res.Body).Decode(&info)
	assert.NoError(err)
	assert.Equal("0123456789abcdef0123456789abcdef01234567", info.Address)

	// Check we can get the full ABI swagger back over REST
	req = httptest.NewRequest("GET", "/abis/message1?swagger", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	var swagger spec.Swagger
	err = json.NewDecoder(res.Body).Decode(&swagger)
	assert.NoError(err)
	assert.Equal("SimpleEvents", swagger.Info.Title)

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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
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

	var abi abiInfo
	json.NewDecoder(res.Body).Decode(&abi)
	assert.NotEmpty(abi.ID)

	req = httptest.NewRequest("POST", "/abis/"+abi.ID+"/0x0123456789abcdef0123456789abcdef01234567?kld-register=testcontract", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	var contract contractInfo
	json.NewDecoder(res.Body).Decode(&contract)
	assert.Equal(201, res.Code)
	assert.Equal("/contracts/testcontract", contract.Path)

	req = httptest.NewRequest("POST", "/abis/"+abi.ID+"/0x0123456789abcdef0123456789abcdef01234567?kld-register=testcontract", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	var errBody map[string]interface{}
	json.NewDecoder(res.Body).Decode(&errBody)
	assert.Equal(409, res.Code)
	assert.Equal("Contract address 0123456789abcdef0123456789abcdef01234567 is already registered for name 'testcontract'", errBody["error"])

	req = httptest.NewRequest("GET", "/contracts/testcontract?swagger", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	returnedSwagger := spec.Swagger{}
	assert.Equal(200, res.Code)
	json.NewDecoder(res.Body).Decode(&returnedSwagger)
	assert.Equal("testcontract", returnedSwagger.Info.Extensions["x-kaleido-registered-name"])
	assert.Equal("/api/v1/contracts/testcontract", returnedSwagger.BasePath)

}

func TestRemoteRegistrySwaggerOrABI(t *testing.T) {
	assert := assert.New(t)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	iMsg := newTestDeployMsg("0123456789abcdef0123456789abcdef01234567")
	iMsg.Headers.ID = "xyz12345"
	rr := &mockRR{
		deployMsg: iMsg,
	}
	scgw.(*smartContractGW).rr = rr

	router := &httprouter.Router{}
	scgw.AddRoutes(router)

	req := httptest.NewRequest("GET", "/g/test?swagger", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	returnedSwagger := spec.Swagger{}
	assert.Equal(200, res.Code)
	json.NewDecoder(res.Body).Decode(&returnedSwagger)
	assert.Equal("/api/v1/g/test", returnedSwagger.BasePath)

	req = httptest.NewRequest("GET", "/i/test?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	returnedSwagger = spec.Swagger{}
	assert.Equal(200, res.Code)
	json.NewDecoder(res.Body).Decode(&returnedSwagger)
	assert.Equal("/api/v1/i/test", returnedSwagger.BasePath)

	req = httptest.NewRequest("GET", "/instances/test", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Code)
	jsonRes := make(map[string]interface{})
	json.NewDecoder(res.Body).Decode(&jsonRes)
	assert.Equal("xyz12345", jsonRes["id"].(string))
	assert.Equal("0123456789abcdef0123456789abcdef01234567", jsonRes["address"].(string))

	req = httptest.NewRequest("GET", "/i/test?ui", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Code)
	html, _ := ioutil.ReadAll(res.Body)
	assert.Contains(string(html), "<html>")
	assert.Contains(string(html), "/instances/test?swagger")

	req = httptest.NewRequest("GET", "/g/test?ui&from=0x12345", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Code)
	html, _ = ioutil.ReadAll(res.Body)
	assert.Contains(string(html), "<html>")
	assert.Contains(string(html), "/gateways/test?swagger")
	assert.Contains(string(html), "0x12345")

	req = httptest.NewRequest("GET", "/g/test?ui&from=0x12345&factory", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Code)
	html, _ = ioutil.ReadAll(res.Body)
	assert.Contains(string(html), "<html>")
	assert.Contains(string(html), "Factory API")

	rr.deployMsg = nil

	req = httptest.NewRequest("GET", "/instances/test?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Code)

	req = httptest.NewRequest("GET", "/gateways/test?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Code)

	rr.err = fmt.Errorf("pop")
	req = httptest.NewRequest("GET", "/instances/test?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(500, res.Code)

	req = httptest.NewRequest("GET", "/gateways/test?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(500, res.Code)

	scgw.Shutdown()
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
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

	var abi abiInfo
	json.NewDecoder(res.Body).Decode(&abi)
	assert.NotEmpty(abi.ID)

	req = httptest.NewRequest("POST", "/abis/"+abi.ID+"/0x0123456789abcdef0123456789abcdef01234567", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	var contract contractInfo
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	router := &httprouter.Router{}
	scgw.AddRoutes(router)

	req := httptest.NewRequest("POST", "/abis/BADID/0x0123456789abcdef0123456789abcdef01234567", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Code)
	var resBody map[string]interface{}
	json.NewDecoder(res.Body).Decode(&resBody)
	assert.Regexp("No ABI found with ID BADID", resBody["error"])
}

func TestLoadDeployMsgOKNoABIInIndex(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	goodMsg := &kldmessages.DeployContract{}
	deployBytes, _ := json.Marshal(goodMsg)
	scgw.abiIndex["abi1"] = &abiInfo{}
	ioutil.WriteFile(path.Join(dir, "abi_abi1.deploy.json"), deployBytes, 0644)
	_, _, err := scgw.loadDeployMsgByID("abi1")
	assert.NoError(err)
}

func TestLoadDeployMsgMissing(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	_, _, err := scgw.loadDeployMsgByID("abi1")
	assert.Regexp("No ABI found with ID abi1", err.Error())
}

func TestLoadDeployMsgFailure(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	scgw.abiIndex["abi1"] = &abiInfo{}
	ioutil.WriteFile(path.Join(dir, "abi_abi1.deploy.json"), []byte(":bad json"), 0644)
	_, _, err := scgw.loadDeployMsgByID("abi1")
	assert.Regexp("Failed to parse ABI with ID abi1", err.Error())
}

func TestLoadDeployMsgRemoteLookupNotFound(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	rr := &mockRR{}
	scgw.rr = rr
	_, _, err := scgw.loadDeployMsgByID("abi1")
	assert.EqualError(err, "No ABI found with ID abi1")
}

func TestPreDeployCompileFailure(t *testing.T) {
	assert := assert.New(t)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: "/anypath",
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	msg := &kldmessages.DeployContract{
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	msg := &kldmessages.DeployContract{
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	contractAddr := common.HexToAddress("0x0123456789AbcdeF0123456789abCdef01234567")
	scgw := s.(*smartContractGW)
	replyMsg := &kldmessages.TransactionReceipt{
		ReplyCommon: kldmessages.ReplyCommon{
			Headers: kldmessages.ReplyHeaders{
				CommonHeaders: kldmessages.CommonHeaders{
					MsgType: kldmessages.MsgTypeTransactionSuccess,
				},
				ReqID: "message1",
			},
		},
		ContractAddress: &contractAddr,
	}

	deployFile := path.Join(dir, "abi_message1.deploy.json")
	deployMsg := &kldmessages.DeployContract{}
	deployBytes, _ := json.Marshal(deployMsg)
	ioutil.WriteFile(deployFile, deployBytes, 0644)
	err := scgw.PostDeploy(replyMsg)
	assert.NoError(err)

	contractInfo := scgw.contractIndex["0123456789abcdef0123456789abcdef01234567"].(*contractInfo)
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	rr := &mockRR{}
	s.(*smartContractGW).rr = rr

	contractAddr := common.HexToAddress("0x0123456789AbcdeF0123456789abCdef01234567")
	scgw := s.(*smartContractGW)
	replyMsg := &kldmessages.TransactionReceipt{
		ReplyCommon: kldmessages.ReplyCommon{
			Headers: kldmessages.ReplyHeaders{
				CommonHeaders: kldmessages.CommonHeaders{
					Context: map[string]interface{}{
						remoteRegistryContextKey: true,
					},
					MsgType: kldmessages.MsgTypeTransactionSuccess,
				},
				ReqID: "message1",
			},
		},
		ContractAddress: &contractAddr,
		RegisterAs:      "lobster",
	}

	deployFile := path.Join(dir, "abi_message1.deploy.json")
	deployMsg := &kldmessages.DeployContract{}
	deployBytes, _ := json.Marshal(deployMsg)
	ioutil.WriteFile(deployFile, deployBytes, 0644)
	err := scgw.PostDeploy(replyMsg)
	assert.NoError(err)

	assert.Equal("0x0123456789abcdef0123456789abcdef01234567", rr.addrCapture)
	assert.Equal("http://localhost/api/v1/instances/lobster?openapi", replyMsg.ContractSwagger)
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	rr := &mockRR{}
	s.(*smartContractGW).rr = rr

	contractAddr := common.HexToAddress("0x0123456789AbcdeF0123456789abCdef01234567")
	scgw := s.(*smartContractGW)
	replyMsg := &kldmessages.TransactionReceipt{
		ReplyCommon: kldmessages.ReplyCommon{
			Headers: kldmessages.ReplyHeaders{
				CommonHeaders: kldmessages.CommonHeaders{
					Context: map[string]interface{}{
						remoteRegistryContextKey: true,
					},
					MsgType: kldmessages.MsgTypeTransactionFailure,
				},
				ReqID: "message1",
			},
		},
		ContractAddress: &contractAddr,
		RegisterAs:      "lobster",
	}

	deployFile := path.Join(dir, "abi_message1.deploy.json")
	deployMsg := &kldmessages.DeployContract{}
	deployBytes, _ := json.Marshal(deployMsg)
	ioutil.WriteFile(deployFile, deployBytes, 0644)
	err := scgw.PostDeploy(replyMsg)
	assert.NoError(err)

	assert.Empty(rr.addrCapture)
	assert.Empty(replyMsg.ContractSwagger)
}

func TestPostDeployMissingContractAddress(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	replyMsg := &kldmessages.TransactionReceipt{
		ReplyCommon: kldmessages.ReplyCommon{
			Headers: kldmessages.ReplyHeaders{
				ReqID: "message1",
			},
		},
	}
	ioutil.WriteFile(path.Join(dir, "message1.deploy.json"), []byte("invalid json"), 0664)

	err := scgw.PostDeploy(replyMsg)
	assert.Regexp("message1: Missing contract address in receipt", err.Error())
}

func TestStoreABIWriteFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: path.Join(dir, "badpath"),
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	i := &contractInfo{
		Address: "req1",
	}
	err := scgw.storeContractInfo(i)
	assert.Regexp("Failed to write ABI JSON", err.Error())
}

func TestLoadABIForInstanceUnknown(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: path.Join(dir, "badpath"),
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	_, _, err := scgw.loadDeployMsgForInstance("invalid")
	assert.Regexp("No contract instance registered with address invalid", err.Error())
}

func TestLoadABIBadData(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	ioutil.WriteFile(path.Join(dir, "badness.abi.json"), []byte(":not json"), 0644)
	_, _, err := scgw.loadDeployMsgByID("badness")
	assert.Regexp("No ABI found with ID badness", err.Error())
}

func TestBuildIndex(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	// Migration of legacy contract interfaces

	var emptySwagger spec.Swagger
	swaggerBytes, _ := json.Marshal(&emptySwagger)
	ioutil.WriteFile(path.Join(dir, "contract_0123456789abcdef0123456789abcdef01234567.swagger.json"), swaggerBytes, 0644)

	okSwagger := spec.Swagger{
		SwaggerProps: spec.SwaggerProps{
			Info: &spec.Info{
				InfoProps: spec.InfoProps{
					Title: "good one",
				},
			},
		},
	}
	okSwagger.Info.AddExtension("x-kaleido-deployment-id", "840b629f-2e46-413b-9671-553a886ca7bb")
	swaggerBytes, _ = json.Marshal(&okSwagger)
	ioutil.WriteFile(path.Join(dir, "contract_123456789abcdef0123456789abcdef012345678.swagger.json"), swaggerBytes, 0644)

	regSwagger := spec.Swagger{
		SwaggerProps: spec.SwaggerProps{
			Info: &spec.Info{
				InfoProps: spec.InfoProps{
					Title: "good one",
				},
			},
		},
	}
	regSwagger.Info.AddExtension("x-kaleido-deployment-id", "840b629f-2e46-413b-9671-553a886ca7bb")
	regSwagger.Info.AddExtension("x-kaleido-registered-name", "migratedcontract")
	swaggerBytes, _ = json.Marshal(&regSwagger)
	ioutil.WriteFile(path.Join(dir, "contract_23456789abcdef0123456789abcdef0123456789.swagger.json"), swaggerBytes, 0644)

	ioutil.WriteFile(path.Join(dir, "contract_3456789abcdef0123456789abcdef01234567890.swagger.json"), []byte(":bad swagger"), 0644)

	// New contract interfaces
	info1 := &contractInfo{
		Address:      "456789abcdef0123456789abcdef012345678901",
		ABI:          "840b629f-2e46-413b-9671-553a886ca7bb",
		Path:         "/contracts/456789abcdef0123456789abcdef012345678901",
		SwaggerURL:   "http://localhost:8080/contracts/456789abcdef0123456789abcdef012345678901?swagger",
		RegisteredAs: "",
		TimeSorted: kldmessages.TimeSorted{
			CreatedISO8601: time.Now().UTC().Format(time.RFC3339),
		},
	}
	info1Bytes, _ := json.Marshal(info1)
	ioutil.WriteFile(path.Join(dir, "contract_456789abcdef0123456789abcdef012345678901.instance.json"), info1Bytes, 0644)
	info2 := &contractInfo{
		Address:      "56789abcdef0123456789abcdef0123456789012",
		ABI:          "840b629f-2e46-413b-9671-553a886ca7bb",
		Path:         "/contracts/somecontract",
		SwaggerURL:   "http://localhost:8080/contracts/somecontract?swagger",
		RegisteredAs: "somecontract",
		TimeSorted: kldmessages.TimeSorted{
			CreatedISO8601: time.Now().UTC().Format(time.RFC3339),
		},
	}
	info2Bytes, _ := json.Marshal(info2)
	ioutil.WriteFile(path.Join(dir, "contract_56789abcdef0123456789abcdef0123456789012.instance.json"), info2Bytes, 0644)

	deployMsg := &kldmessages.DeployContract{
		ContractName: "abideployable",
	}
	deployBytes, _ := json.Marshal(&deployMsg)
	ioutil.WriteFile(path.Join(dir, "abi_840b629f-2e46-413b-9671-553a886ca7bb.deploy.json"), deployBytes, 0644)
	ioutil.WriteFile(path.Join(dir, "abi_e27be4cf-6ae2-411e-8088-db2992618938.deploy.json"), deployBytes, 0644)
	ioutil.WriteFile(path.Join(dir, "abi_519526b2-0879-41f4-93c0-09acaa62e2da.deploy.json"), []byte(":bad json"), 0644)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	assert.Equal(4, len(scgw.contractIndex))
	info := scgw.contractIndex["123456789abcdef0123456789abcdef012345678"].(*contractInfo)
	assert.Equal("123456789abcdef0123456789abcdef012345678", info.Address)

	req := httptest.NewRequest("GET", "/contracts", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	params := httprouter.Params{}
	scgw.listContractsOrABIs(res, req, params)
	assert.Equal(200, res.Result().StatusCode)
	var contractInfos []*contractInfo
	err := json.NewDecoder(res.Body).Decode(&contractInfos)
	assert.NoError(err)
	assert.Equal(4, len(contractInfos))
	assert.Equal("123456789abcdef0123456789abcdef012345678", contractInfos[0].Address)
	assert.Equal("23456789abcdef0123456789abcdef0123456789", contractInfos[1].Address)
	assert.Equal("456789abcdef0123456789abcdef012345678901", contractInfos[2].Address)
	assert.Equal("56789abcdef0123456789abcdef0123456789012", contractInfos[3].Address)

	somecontractAddr, err := scgw.resolveContractAddr("somecontract")
	assert.NoError(err)
	assert.Equal("56789abcdef0123456789abcdef0123456789012", somecontractAddr)

	migratedcontractAddr, err := scgw.resolveContractAddr("migratedcontract")
	assert.NoError(err)
	assert.Equal("23456789abcdef0123456789abcdef0123456789", migratedcontractAddr)

	req = httptest.NewRequest("GET", "/abis", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	params = httprouter.Params{}
	scgw.listContractsOrABIs(res, req, params)
	assert.Equal(200, res.Result().StatusCode)
	var abiInfos []*abiInfo
	err = json.NewDecoder(res.Body).Decode(&abiInfos)
	assert.NoError(err)
	assert.Equal(2, len(abiInfos))
	assert.Equal("840b629f-2e46-413b-9671-553a886ca7bb", abiInfos[0].ID)
	assert.Equal("e27be4cf-6ae2-411e-8088-db2992618938", abiInfos[1].ID)
}

func TestGetContractOrABIFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	scgw.contractIndex["123456789abcdef0123456789abcdef012345678"] = &contractInfo{
		Address: "123456789abcdef0123456789abcdef012345678",
	}
	scgw.abiIndex["badabi"] = &abiInfo{}

	// One that exists in the index, but for some reason the file isn't there
	req := httptest.NewRequest("GET", "/contracts/123456789abcdef0123456789abcdef012345678?openapi", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Result().StatusCode)

	// One that does not exist in the index
	req = httptest.NewRequest("GET", "/contracts/nonexistent?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router = &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Result().StatusCode)

	// One that exists in the index, but for some reason the file isn't there
	req = httptest.NewRequest("GET", "/abis/badabi?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router = &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Result().StatusCode)

	// One that simply doesn't exist in the index - should be a 404
	req = httptest.NewRequest("GET", "/abis/23456789abcdef0123456789abcdef0123456789?openapi", bytes.NewReader([]byte{}))
	res = httptest.NewRecorder()
	router = &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)
	assert.Equal(404, res.Result().StatusCode)
}

func TestGetContractUI(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	scgw.contractIndex["123456789abcdef0123456789abcdef012345678"] = &contractInfo{
		ABI:     "abi1",
		Address: "123456789abcdef0123456789abcdef012345678",
	}
	ioutil.WriteFile(path.Join(dir, "abi_abi1.deploy.json"), []byte("{}"), 0644)
	scgw.abiIndex["abi1"] = &abiInfo{}

	req := httptest.NewRequest("GET", "/contracts/123456789abcdef0123456789abcdef012345678?ui", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)
	assert.Equal(200, res.Result().StatusCode)
	body, _ := ioutil.ReadAll(res.Body)
	assert.Regexp("Ethconnect REST Gateway", string(body))
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
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
	info := &abiInfo{}
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
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
	info := &abiInfo{}
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
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
	errInfo := &restErrMsg{}
	err := json.NewDecoder(res.Body).Decode(errInfo)
	assert.NoError(err)
	assert.Regexp("Error unarchiving supplied zip file", errInfo.Message)
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
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
	errInfo := &restErrMsg{}
	err := json.NewDecoder(res.Body).Decode(errInfo)
	assert.NoError(err)
	assert.Equal("Failed to compile solidity: No .sol files found in root. Please set a 'source' query param or form field to the relative path of your solidity", errInfo.Message)
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	req := httptest.NewRequest("POST", "/abis", bytes.NewReader([]byte{}))
	res := httptest.NewRecorder()
	router := &httprouter.Router{}
	scgw.AddRoutes(router)
	router.ServeHTTP(res, req)

	assert.Equal(400, res.Result().StatusCode)
	errInfo := &restErrMsg{}
	err := json.NewDecoder(res.Body).Decode(errInfo)
	assert.NoError(err)
	assert.Equal("Could not parse supplied multi-part form data: request Content-Type isn't multipart/form-data", errInfo.Message)
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	_, err := scgw.compileMultipartFormSolidity(path.Join(dir, "baddir"), nil)
	assert.EqualError(err, "Failed to read extracted multi-part form data")
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)
	os.Setenv("KLD_SOLC_0_99", "badness")

	ioutil.WriteFile(path.Join(dir, "solidity.sol"), []byte(simpleEventsSource()), 0644)
	req := httptest.NewRequest("POST", "/abis?compiler=0.99", bytes.NewReader([]byte{}))
	_, err := scgw.compileMultipartFormSolidity(dir, req)
	assert.Regexp("Failed checking solc version", err.Error())
	os.Unsetenv("KLD_SOLC_0_99")
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	ioutil.WriteFile(path.Join(dir, "solidity.sol"), []byte(simpleEventsSource()), 0644)
	req := httptest.NewRequest("POST", "/abis?compiler=0.99", bytes.NewReader([]byte{}))
	_, err := scgw.compileMultipartFormSolidity(dir, req)
	assert.EqualError(err, "Failed checking solc version: Could not find a configured compiler for requested Solidity major version 0.99")
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	err := scgw.extractMultiPartFile(dir, &multipart.FileHeader{
		Filename: "/stuff.zip",
	})
	assert.EqualError(err, "Filenames cannot contain slashes. Use a zip file to upload a directory structure")
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	err := scgw.extractMultiPartFile(dir, &multipart.FileHeader{
		Filename: "stuff.zip",
	})
	assert.EqualError(err, "Failed to read archive")
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
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	_, err := scgw.storeDeployableABI(&kldmessages.DeployContract{}, nil)
	assert.EqualError(err, "Must supply ABI to install an existing ABI into the REST Gateway")
}

func TestAddFileToContractIndexBadFileSwallowsError(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	scgw.addFileToContractIndex("", "badness")
}

func TestAddFileToContractIndexBadDataSwallowsError(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	fileName := path.Join(dir, "badness")
	ioutil.WriteFile(fileName, []byte("!JSON"), 0644)
	scgw.addFileToContractIndex("", fileName)
}

func TestAddFileToABIIndexBadFileSwallowsError(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)

	s, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: true,
		},
		nil, nil, nil,
	)
	scgw := s.(*smartContractGW)

	scgw.addFileToABIIndex("", "badness", time.Now().UTC())
}

func testGWPath(method, path string, results interface{}, sm *mockSubMgr) (res *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, nil)
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
	res := testGWPath("POST", kldevents.StreamPathPrefix, nil, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestAddStreamOK(t *testing.T) {
	assert := assert.New(t)
	spec := &kldevents.StreamInfo{Type: "webhook"}
	b, _ := json.Marshal(spec)
	req := httptest.NewRequest("POST", kldevents.StreamPathPrefix, bytes.NewReader(b))
	res := httptest.NewRecorder()
	s := &smartContractGW{}
	s.sm = &mockSubMgr{}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	var newSpec kldevents.StreamInfo
	json.NewDecoder(res.Body).Decode(&newSpec)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("webhook", newSpec.Type)
	s.Shutdown()
}

func TestAddStreamBadData(t *testing.T) {
	assert := assert.New(t)
	req := httptest.NewRequest("POST", kldevents.StreamPathPrefix, bytes.NewReader([]byte(":bad json")))
	res := httptest.NewRecorder()
	s := &smartContractGW{}
	s.sm = &mockSubMgr{}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	var resError restErrMsg
	json.NewDecoder(res.Body).Decode(&resError)
	assert.Equal(400, res.Result().StatusCode)
	assert.Regexp("Invalid event stream specification", resError.Message)
}

func TestAddStreamSubMgrError(t *testing.T) {
	assert := assert.New(t)
	spec := &kldevents.StreamInfo{Type: "webhook"}
	b, _ := json.Marshal(spec)
	req := httptest.NewRequest("POST", kldevents.StreamPathPrefix, bytes.NewReader(b))
	res := httptest.NewRecorder()
	s := &smartContractGW{}
	s.sm = &mockSubMgr{err: fmt.Errorf("pop")}
	r := &httprouter.Router{}
	s.AddRoutes(r)
	r.ServeHTTP(res, req)
	var resError restErrMsg
	json.NewDecoder(res.Body).Decode(&resError)
	assert.Equal(400, res.Result().StatusCode)
	assert.Regexp("pop", resError.Message)
}

func TestListStreamsNoSubMgr(t *testing.T) {
	assert := assert.New(t)
	res := testGWPath("GET", kldevents.StreamPathPrefix, nil, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestListStreams(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{
		streams: []*kldevents.StreamInfo{
			&kldevents.StreamInfo{
				TimeSorted: kldmessages.TimeSorted{
					CreatedISO8601: time.Now().UTC().Format(time.RFC3339),
				}, ID: "earlier",
			},
			&kldevents.StreamInfo{
				TimeSorted: kldmessages.TimeSorted{
					CreatedISO8601: time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339),
				}, ID: "later",
			},
		},
	}
	var results []*kldevents.StreamInfo
	res := testGWPath("GET", kldevents.StreamPathPrefix, &results, mockSubMgr)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(2, len(results))
	assert.Equal("later", results[0].ID)
	assert.Equal("earlier", results[1].ID)
}

func TestListSubs(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{
		subs: []*kldevents.SubscriptionInfo{
			&kldevents.SubscriptionInfo{
				TimeSorted: kldmessages.TimeSorted{
					CreatedISO8601: time.Now().UTC().Format(time.RFC3339),
				}, ID: "earlier",
			},
			&kldevents.SubscriptionInfo{
				TimeSorted: kldmessages.TimeSorted{
					CreatedISO8601: time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339),
				}, ID: "later",
			},
		},
	}
	var results []*kldevents.SubscriptionInfo
	res := testGWPath("GET", kldevents.SubPathPrefix, &results, mockSubMgr)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal(2, len(results))
	assert.Equal("later", results[0].ID)
	assert.Equal("earlier", results[1].ID)
}

func TestGetSub(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{
		sub: &kldevents.SubscriptionInfo{ID: "123"},
	}
	var result kldevents.SubscriptionInfo
	res := testGWPath("GET", kldevents.SubPathPrefix+"/123", &result, mockSubMgr)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("123", result.ID)
}

func TestGetStream(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{
		stream: &kldevents.StreamInfo{ID: "123"},
	}
	var result kldevents.StreamInfo
	res := testGWPath("GET", kldevents.StreamPathPrefix+"/123", &result, mockSubMgr)
	assert.Equal(200, res.Result().StatusCode)
	assert.Equal("123", result.ID)
}

func TestGetSubNoSubMgr(t *testing.T) {
	assert := assert.New(t)

	var result kldevents.SubscriptionInfo
	res := testGWPath("GET", kldevents.SubPathPrefix+"/123", &result, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestGetSubNotFound(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{err: fmt.Errorf("not found")}
	var result kldevents.SubscriptionInfo
	res := testGWPath("GET", kldevents.SubPathPrefix+"/123", &result, mockSubMgr)
	assert.Equal(404, res.Result().StatusCode)
}

func TestDeleteSub(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{}
	res := testGWPath("DELETE", kldevents.SubPathPrefix+"/123", nil, mockSubMgr)
	assert.Equal(204, res.Result().StatusCode)
}

func TestDeleteStream(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{}
	res := testGWPath("DELETE", kldevents.StreamPathPrefix+"/123", nil, mockSubMgr)
	assert.Equal(204, res.Result().StatusCode)
}

func TestDeleteSubNoSubMgr(t *testing.T) {
	assert := assert.New(t)

	res := testGWPath("DELETE", kldevents.SubPathPrefix+"/123", nil, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestDeleteSubError(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{err: fmt.Errorf("not found")}
	var errInfo = restErrMsg{}
	res := testGWPath("DELETE", kldevents.SubPathPrefix+"/123", &errInfo, mockSubMgr)
	assert.Equal(500, res.Result().StatusCode)
	assert.Equal("not found", errInfo.Message)
}

func TestSuspendStream(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{}
	res := testGWPath("POST", kldevents.StreamPathPrefix+"/123/suspend", nil, mockSubMgr)
	assert.Equal(204, res.Result().StatusCode)
	assert.True(mockSubMgr.suspended)
}

func TestResumeStream(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{}
	res := testGWPath("POST", kldevents.StreamPathPrefix+"/123/resume", nil, mockSubMgr)
	assert.Equal(204, res.Result().StatusCode)
	assert.True(mockSubMgr.resumed)
}

func TestResumeStreamFail(t *testing.T) {
	assert := assert.New(t)

	mockSubMgr := &mockSubMgr{err: fmt.Errorf("pop")}
	var errInfo = restErrMsg{}
	res := testGWPath("POST", kldevents.StreamPathPrefix+"/123/resume", &errInfo, mockSubMgr)
	assert.Equal(500, res.Result().StatusCode)
	assert.Equal("pop", errInfo.Message)
}

func TestSuspendNoSubMgr(t *testing.T) {
	assert := assert.New(t)

	res := testGWPath("POST", kldevents.StreamPathPrefix+"/123/resume", nil, nil)
	assert.Equal(405, res.Result().StatusCode)
}

func TestCheckNameAvailableRRDuplicate(t *testing.T) {
	assert := assert.New(t)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	rr := &mockRR{
		deployMsg: newTestDeployMsg("12345"),
	}
	s := scgw.(*smartContractGW)
	s.rr = rr

	err := s.checkNameAvailable("lobster", true)
	assert.EqualError(err, "Contract address 12345 is already registered for name 'lobster'")
}

func TestCheckNameAvailableRRFail(t *testing.T) {
	assert := assert.New(t)

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
	)
	rr := &mockRR{
		err: fmt.Errorf("pop"),
	}
	s := scgw.(*smartContractGW)
	s.rr = rr

	err := s.checkNameAvailable("lobster", true)
	assert.EqualError(err, "pop")
}

func TestWithEventsAuthRequiresAuth(t *testing.T) {
	assert := assert.New(t)

	kldauth.RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	scgw, _ := NewSmartContractGateway(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
		},
		&kldtx.TxnProcessorConf{
			OrionPrivateAPIS: false,
		},
		nil, nil, nil,
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

	kldauth.RegisterSecurityModule(nil)
}
