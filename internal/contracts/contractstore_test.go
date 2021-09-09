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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path"
	"testing"
	"time"

	"github.com/hyperledger-labs/firefly-ethconnect/internal/messages"
	"github.com/stretchr/testify/assert"
)

func TestLoadDeployMsgOKNoABIInIndex(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	cs := newContractStore(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		nil,
	)
	goodMsg := &messages.DeployContract{}
	deployBytes, _ := json.Marshal(goodMsg)
	cs.(*contractStore).abiIndex["abi1"] = &abiInfo{}
	ioutil.WriteFile(path.Join(dir, "abi_abi1.deploy.json"), deployBytes, 0644)
	_, _, err := cs.loadDeployMsgByID("abi1")
	assert.NoError(err)
}

func TestLoadDeployMsgMissing(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	cs := newContractStore(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		nil,
	)
	_, _, err := cs.loadDeployMsgByID("abi1")
	assert.Regexp("No ABI found with ID abi1", err.Error())
}

func TestLoadDeployMsgFileMissing(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	cs := newContractStore(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		nil,
	)
	cs.(*contractStore).abiIndex["abi1"] = &abiInfo{}
	_, _, err := cs.loadDeployMsgByID("abi1")
	assert.Regexp("Failed to load ABI with ID abi1", err.Error())
}

func TestLoadDeployMsgFailure(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	cs := newContractStore(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		nil,
	)
	cs.(*contractStore).abiIndex["abi1"] = &abiInfo{}
	ioutil.WriteFile(path.Join(dir, "abi_abi1.deploy.json"), []byte(":bad json"), 0644)
	_, _, err := cs.loadDeployMsgByID("abi1")
	assert.Regexp("Failed to parse ABI with ID abi1", err.Error())
}

func TestLoadDeployMsgRemoteLookupNotFound(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	cs := newContractStore(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		nil,
	)
	rr := &mockRR{}
	cs.(*contractStore).rr = rr
	_, _, err := cs.loadDeployMsgByID("abi1")
	assert.EqualError(err, "No ABI found with ID abi1")
}

func TestStoreABIWriteFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	cs := newContractStore(
		&SmartContractGatewayConf{
			StoragePath: path.Join(dir, "badpath"),
		},
		nil,
	)

	i := &contractInfo{
		Address: "req1",
	}
	err := cs.(*contractStore).storeContractInfo(i)
	assert.Regexp("Failed to write ABI JSON", err.Error())
}

func TestLoadABIForInstanceUnknown(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	cs := newContractStore(
		&SmartContractGatewayConf{
			StoragePath: path.Join(dir, "badpath"),
		},
		nil,
	)

	_, err := cs.lookupContractInstance("invalid")
	assert.Regexp("No contract instance registered with address invalid", err.Error())
}

func TestLoadABIBadData(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	cs := newContractStore(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		nil,
	)

	ioutil.WriteFile(path.Join(dir, "badness.abi.json"), []byte(":not json"), 0644)
	_, _, err := cs.loadDeployMsgByID("badness")
	assert.Regexp("No ABI found with ID badness", err.Error())
}

func TestAddFileToContractIndexBadFileSwallowsError(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)

	cs := newContractStore(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		nil,
	)

	cs.addFileToContractIndex("", "badness")
}

func TestAddFileToContractIndexBadDataSwallowsError(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)

	cs := newContractStore(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		nil,
	)

	fileName := path.Join(dir, "badness")
	ioutil.WriteFile(fileName, []byte("!JSON"), 0644)
	cs.addFileToContractIndex("", fileName)
}

func TestAddFileToABIIndexBadFileSwallowsError(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)

	cs := newContractStore(
		&SmartContractGatewayConf{
			StoragePath: dir,
		},
		nil,
	)

	cs.(*contractStore).addFileToABIIndex("", "badness", time.Now().UTC())
}

func TestCheckNameAvailableRRDuplicate(t *testing.T) {
	assert := assert.New(t)

	cs := newContractStore(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
		},
		nil,
	)
	rr := &mockRR{
		deployMsg: newTestDeployMsg(t, "12345"),
	}
	cs.(*contractStore).rr = rr

	err := cs.checkNameAvailable("lobster", true)
	assert.EqualError(err, "Contract address 12345 is already registered for name 'lobster'")
}

func TestCheckNameAvailableRRFail(t *testing.T) {
	assert := assert.New(t)

	cs := newContractStore(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
		},
		nil,
	)
	rr := &mockRR{
		err: fmt.Errorf("pop"),
	}
	cs.(*contractStore).rr = rr

	err := cs.checkNameAvailable("lobster", true)
	assert.EqualError(err, "pop")
}

func TestResolveAddressFail(t *testing.T) {
	assert := assert.New(t)

	cs := newContractStore(
		&SmartContractGatewayConf{
			BaseURL: "http://localhost/api/v1",
		},
		nil,
	)

	deployMsg, name, info, err := cs.resolveAddressOrName("test")
	assert.Regexp("No contract instance registered with address test", err)
	assert.Nil(deployMsg)
	assert.Nil(info)
	assert.Equal("", name)
}
