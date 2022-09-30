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

package contractregistry

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path"
	"testing"
	"time"

	"github.com/go-openapi/spec"
	"github.com/hyperledger/firefly-ethconnect/internal/eth"
	"github.com/hyperledger/firefly-ethconnect/internal/messages"
	"github.com/stretchr/testify/assert"
)

type mockRR struct {
	deployMsg *DeployContractWithAddress
	err       error
}

func (rr *mockRR) LoadFactoryForGateway(id string, refresh bool) (*messages.DeployContract, error) {
	if rr.deployMsg == nil {
		return nil, rr.err
	}
	return rr.deployMsg.Contract, rr.err
}
func (rr *mockRR) LoadFactoryForInstance(id string, refresh bool) (*DeployContractWithAddress, error) {
	return rr.deployMsg, rr.err
}
func (rr *mockRR) RegisterInstance(lookupStr, address string) error {
	return rr.err
}
func (rr *mockRR) Close()      {}
func (rr *mockRR) Init() error { return nil }

var simpleEventsSol string

func simpleEventsSource() string {
	if simpleEventsSol == "" {
		simpleEventsBytes, _ := ioutil.ReadFile("../../test/simpleevents.sol")
		simpleEventsSol = string(simpleEventsBytes)
	}
	return simpleEventsSol
}

func newTestDeployMsg(t *testing.T, addr string) *DeployContractWithAddress {
	compiled, err := eth.CompileContract(simpleEventsSource(), "SimpleEvents", "", "")
	assert.NoError(t, err)
	return &DeployContractWithAddress{
		Contract: &messages.DeployContract{ABI: compiled.ABI},
		Address:  addr,
	}
}

func TestLoadDeployMsgOK(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)
	defer cs.Close()

	goodMsg := &messages.DeployContract{
		ContractName: "test",
	}
	_, err = cs.(*contractStore).AddABI("abi1", goodMsg, time.Now())
	assert.NoError(err)
	info, err := cs.GetABI(ABILocation{
		ABIType: LocalABI,
		Name:    "abi1",
	}, false)
	assert.NoError(err)
	assert.Equal("test", info.Contract.ContractName)

	// verify cache hit
	assert.Equal(1, cs.(*contractStore).abiCache.Len())
	_, err = cs.GetABI(ABILocation{
		ABIType: LocalABI,
		Name:    "abi1",
	}, false)
	assert.NoError(err)

	// Verify just getting the info
	abiInfo, err := cs.GetLocalABIInfo("abi1")
	assert.NoError(err)
	assert.Equal("test", abiInfo.Name)
}

func TestLoadDeployMsgMissing(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	_, err = cs.GetABI(ABILocation{
		ABIType: LocalABI,
		Name:    "abi1",
	}, false)
	assert.Regexp("No ABI found with ID abi1", err.Error())
}

func TestLoadDeployMsgFailure(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	cs.(*contractStore).db.Put(fmt.Sprintf("%s/%s", ldbABIIDPrefix, "abi1"), []byte(":bad json"))
	_, err = cs.GetABI(ABILocation{
		ABIType: LocalABI,
		Name:    "abi1",
	}, false)
	assert.Regexp("FFEC100223", err.Error())
}

func TestStoreABIWriteFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	cs := NewContractStore(&ContractStoreConf{StoragePath: path.Join(dir, "badpath")}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	cs.(*contractStore).db.Close()

	i := &ContractInfo{
		Address: "req1",
	}
	err = cs.(*contractStore).storeContractInfo(i)
	assert.Regexp("leveldb", err.Error())
}

func TestLoadABIForInstanceUnknown(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)

	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	_, err = cs.GetContractByAddress("invalid")
	assert.Regexp("No contract instance registered with address invalid", err.Error())
}

func TestCheckNameAvailableRRDuplicate(t *testing.T) {
	assert := assert.New(t)

	mrr := &mockRR{
		deployMsg: newTestDeployMsg(t, "12345"),
	}

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{BaseURL: "http://localhost/api/v1", StoragePath: dir}, mrr)

	err := cs.CheckNameAvailable("lobster", true)
	assert.Regexp("Contract address 12345 is already registered for name 'lobster'", err)
}

func TestCheckNameAvailableRRFail(t *testing.T) {
	assert := assert.New(t)

	mrr := &mockRR{
		err: fmt.Errorf("pop"),
	}
	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{BaseURL: "http://localhost/api/v1", StoragePath: dir}, mrr)

	err := cs.CheckNameAvailable("lobster", true)
	assert.Regexp("pop", err)
}

func TestCheckNameAvailableRROk(t *testing.T) {
	assert := assert.New(t)

	mrr := &mockRR{}
	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{BaseURL: "http://localhost/api/v1", StoragePath: dir}, mrr)

	err := cs.CheckNameAvailable("lobster", true)
	assert.NoError(err)
}

func TestCheckNameAvailableLocalFail(t *testing.T) {
	assert := assert.New(t)

	mrr := &mockRR{}
	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{BaseURL: "http://localhost/api/v1", StoragePath: dir}, mrr)
	cs.Init()

	cs.AddContract("0x12345", "ab1", "name", "lobster")

	err := cs.CheckNameAvailable("lobster", false)
	assert.Regexp("FFEC100133", err)
}

func TestAddRemoteInstance(t *testing.T) {
	assert := assert.New(t)

	mrr := &mockRR{
		err: fmt.Errorf("pop"),
	}
	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{BaseURL: "http://localhost/api/v1", StoragePath: dir}, mrr)
	cs.Init()

	err := cs.AddRemoteInstance("lobster", "0x12345")
	assert.Regexp("pop", err)
}

func TestLDBlBadDir(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir()
	cleanup(dir)
	ioutil.WriteFile(dir, []byte("this is not a directory"), 0644)
	defer cleanup(dir)

	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.Regexp("FFEC100054", err)
}

func TestMigrateFilesToLevelBadDir(t *testing.T) {
	dir := tempdir()
	cleanup(dir)

	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	cs.(*contractStore).migrateFilesToLevelDB()
}

func TestBadCacheMissingStorage(t *testing.T) {
	neg1 := -1
	assert := assert.New(t)
	cs := NewContractStore(&ContractStoreConf{ABICacheSize: &neg1}, &mockRR{})
	err := cs.Init()
	assert.Regexp("FFEC100224", err)
}

func TestBadCacheConfig(t *testing.T) {
	neg1 := -1
	assert := assert.New(t)
	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{ABICacheSize: &neg1, StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.Regexp("FFEC100134", err)
}

func TestMigrateFilesToLevelDB(t *testing.T) {
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
	okSwagger.Info.AddExtension("x-firefly-deployment-id", "840b629f-2e46-413b-9671-553a886ca7bb")
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
	regSwagger.Info.AddExtension("x-firefly-deployment-id", "840b629f-2e46-413b-9671-553a886ca7bb")
	regSwagger.Info.AddExtension("x-firefly-registered-name", "migratedcontract")
	swaggerBytes, _ = json.Marshal(&regSwagger)
	ioutil.WriteFile(path.Join(dir, "contract_23456789abcdef0123456789abcdef0123456789.swagger.json"), swaggerBytes, 0644)

	ioutil.WriteFile(path.Join(dir, "contract_3456789abcdef0123456789abcdef01234567890.swagger.json"), []byte(":bad swagger"), 0644)

	// New contract interfaces
	info1 := &ContractInfo{
		Address:      "456789abcdef0123456789abcdef012345678901",
		ABI:          "840b629f-2e46-413b-9671-553a886ca7bb",
		Path:         "/contracts/456789abcdef0123456789abcdef012345678901",
		SwaggerURL:   "http://localhost:8080/contracts/456789abcdef0123456789abcdef012345678901?swagger",
		RegisteredAs: "",
		TimeSorted: messages.TimeSorted{
			CreatedISO8601: time.Now().UTC().Format(time.RFC3339),
		},
	}
	info1Bytes, _ := json.Marshal(info1)
	ioutil.WriteFile(path.Join(dir, "contract_456789abcdef0123456789abcdef012345678901.instance.json"), info1Bytes, 0644)
	info2 := &ContractInfo{
		Address:      "56789abcdef0123456789abcdef0123456789012",
		ABI:          "840b629f-2e46-413b-9671-553a886ca7bb",
		Path:         "/contracts/somecontract",
		SwaggerURL:   "http://localhost:8080/contracts/somecontract?swagger",
		RegisteredAs: "somecontract",
		TimeSorted: messages.TimeSorted{
			CreatedISO8601: time.Now().UTC().Format(time.RFC3339),
		},
	}
	info2Bytes, _ := json.Marshal(info2)
	ioutil.WriteFile(path.Join(dir, "contract_56789abcdef0123456789abcdef0123456789012.instance.json"), info2Bytes, 0644)

	deployMsg := &messages.DeployContract{
		ContractName: "abideployable",
	}
	deployBytes, _ := json.Marshal(&deployMsg)
	ioutil.WriteFile(path.Join(dir, "abi_840b629f-2e46-413b-9671-553a886ca7bb.deploy.json"), deployBytes, 0644)
	ioutil.WriteFile(path.Join(dir, "abi_e27be4cf-6ae2-411e-8088-db2992618938.deploy.json"), deployBytes, 0644)
	ioutil.WriteFile(path.Join(dir, "abi_519526b2-0879-41f4-93c0-09acaa62e2da.deploy.json"), []byte(":bad json"), 0644)

	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	contracts, err := cs.ListContracts()
	assert.NoError(err)
	assert.Equal(4, len(contracts))
	assert.Equal("123456789abcdef0123456789abcdef012345678", contracts[0].(*ContractInfo).Address)
	assert.Equal("23456789abcdef0123456789abcdef0123456789", contracts[1].(*ContractInfo).Address)
	assert.Equal("456789abcdef0123456789abcdef012345678901", contracts[2].(*ContractInfo).Address)
	assert.Equal("56789abcdef0123456789abcdef0123456789012", contracts[3].(*ContractInfo).Address)

	info, err := cs.GetContractByAddress("123456789abcdef0123456789abcdef012345678")
	assert.NoError(err)
	assert.Equal("123456789abcdef0123456789abcdef012345678", info.Address)

	somecontractAddr, err := cs.ResolveContractAddress("somecontract")
	assert.NoError(err)
	assert.Equal("56789abcdef0123456789abcdef0123456789012", somecontractAddr)

	migratedcontractAddr, err := cs.ResolveContractAddress("migratedcontract")
	assert.NoError(err)
	assert.Equal("23456789abcdef0123456789abcdef0123456789", migratedcontractAddr)

	abis, err := cs.ListABIs()
	assert.NoError(err)
	assert.Equal(2, len(abis))
	assert.Equal("840b629f-2e46-413b-9671-553a886ca7bb", abis[0].(*ABIInfo).ID)
	assert.Equal("e27be4cf-6ae2-411e-8088-db2992618938", abis[1].(*ABIInfo).ID)
}

func TestGetABIRemoteGateway(t *testing.T) {
	assert := assert.New(t)

	mrr := &mockRR{
		deployMsg: &DeployContractWithAddress{
			Contract: &messages.DeployContract{
				Description: "description",
			},
			Address: "address",
		},
	}

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, mrr)
	err := cs.Init()
	assert.NoError(err)

	location := ABILocation{ABIType: RemoteGateway}
	deployMsg, err := cs.GetABI(location, false)
	assert.NoError(err)
	assert.Equal("", deployMsg.Address)
	assert.Equal("description", deployMsg.Contract.Description)
}

func TestGetABIRemoteInstance(t *testing.T) {
	assert := assert.New(t)

	mrr := &mockRR{
		deployMsg: &DeployContractWithAddress{
			Contract: &messages.DeployContract{
				Description: "description",
			},
			Address: "address",
		},
	}

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, mrr)
	err := cs.Init()
	assert.NoError(err)

	location := ABILocation{ABIType: RemoteInstance}
	deployMsg, err := cs.GetABI(location, false)
	assert.NoError(err)
	assert.Equal("address", deployMsg.Address)
	assert.Equal("description", deployMsg.Contract.Description)

	// verify cache hit
	assert.Equal(1, cs.(*contractStore).abiCache.Len())
	mrr.deployMsg = nil
	deployMsg, err = cs.GetABI(location, false)
	assert.NoError(err)
	assert.Equal("address", deployMsg.Address)
	assert.Equal("description", deployMsg.Contract.Description)

	// bad type
	assert.Panics(func() {
		_, _ = cs.GetABI(ABILocation{ABIType: abiType(99)}, false)
	})

}

func TestGetABIRemoteInstanceFail(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	location := ABILocation{ABIType: RemoteInstance}
	deployMsg, err := cs.GetABI(location, false)
	assert.NoError(err)
	assert.Nil(deployMsg)
}

func TestGetABILocalFail(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	location := ABILocation{ABIType: LocalABI, Name: "test"}
	deployMsg, err := cs.GetABI(location, false)
	assert.Regexp("No ABI found with ID test", err)
	assert.Nil(deployMsg)
}

func TestAddContractFail(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	cs.(*contractStore).db.Close()

	_, err = cs.AddContract("0x123456789abcdef0123456789abcdef012345678", "abcd1234", "name", "name")
	assert.Error(err)
}

func TestResolveContractByNameNotFound(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	_, err = cs.ResolveContractAddress("name")
	assert.Regexp("FFEC100125", err)
}

func TestResolveContractByNameFail(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	err = cs.(*contractStore).storeContractInfo(&ContractInfo{
		RegisteredAs: "test",
		Address:      "0x123456789abcdef0123456789abcdef012345678",
	})
	assert.NoError(err)

	cs.(*contractStore).db.Close()

	_, err = cs.ResolveContractAddress("name")
	assert.Error(err)
}

func TestGetContractByAddressFail(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	err = cs.(*contractStore).storeContractInfo(&ContractInfo{
		RegisteredAs: "test",
		Address:      "0x123456789abcdef0123456789abcdef012345678",
	})
	assert.NoError(err)

	cs.(*contractStore).db.Close()

	_, err = cs.GetContractByAddress("0x123456789abcdef0123456789abcdef012345678")
	assert.Error(err)
}

func TestGetAddABIFail(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	cs.(*contractStore).db.Close()

	_, err = cs.AddABI("abi1", &messages.DeployContract{}, time.Now())
	assert.Error(err)
}

func TestIsRemote(t *testing.T) {
	assert := assert.New(t)

	result := IsRemote(messages.CommonHeaders{
		Context: map[string]interface{}{
			RemoteRegistryContextKey: true,
		},
	})
	assert.Equal(true, result)

	result = IsRemote(messages.CommonHeaders{
		Context: map[string]interface{}{
			RemoteRegistryContextKey: false,
		},
	})
	assert.Equal(false, result)

	result = IsRemote(messages.CommonHeaders{
		Context: map[string]interface{}{},
	})
	assert.Equal(false, result)
}

func TestMigrateCleanupFail(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	cs.(*contractStore).cleanupMigratedFile(path.Join(dir, "missing.json"))

}

func TestMigrateABIFileFail(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	cs.(*contractStore).migrateABIFile("abi1", path.Join(dir, "missing.json"), time.Now())

	cs.(*contractStore).db.Close()

	ioutil.WriteFile(path.Join(dir, "ok.json"), []byte("{}"), 0644)

	cs.(*contractStore).migrateABIFile("abi1", path.Join(dir, "ok.json"), time.Now())

}

func TestMigrateContractFileFail(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	cs.(*contractStore).migrateContractFile("0x12345", path.Join(dir, "missing.json"), time.Now())

	ioutil.WriteFile(path.Join(dir, "badjson.json"), []byte("!bad json}"), 0644)

	cs.(*contractStore).migrateContractFile("0x12345", path.Join(dir, "badjson.json"), time.Now())

	cs.(*contractStore).db.Close()

	ioutil.WriteFile(path.Join(dir, "ok.json"), []byte("{}"), 0644)

	cs.(*contractStore).migrateContractFile("0x12345", path.Join(dir, "ok.json"), time.Now())

}

func TestMigrateLegacyContractFileFail(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	cs.(*contractStore).migrateLegacyContractFile("0x12345", path.Join(dir, "missing.json"), time.Now())

	ioutil.WriteFile(path.Join(dir, "badjson.json"), []byte("!bad json}"), 0644)

	cs.(*contractStore).migrateLegacyContractFile("0x12345", path.Join(dir, "badjson.json"), time.Now())

	ioutil.WriteFile(path.Join(dir, "no_ext.json"), []byte(`{"info":{}}`), 0644)

	cs.(*contractStore).migrateLegacyContractFile("0x12345", path.Join(dir, "no_ext.json"), time.Now())

	cs.(*contractStore).db.Close()

	ioutil.WriteFile(path.Join(dir, "ok.json"), []byte(`{"info":{ "x-firefly-registered-name": "myname", "x-firefly-deployment-id": "12345" }}`), 0644)

	cs.(*contractStore).migrateLegacyContractFile("0x12345", path.Join(dir, "ok.json"), time.Now())

}

func TestListBadJSON(t *testing.T) {
	assert := assert.New(t)

	dir := tempdir()
	defer cleanup(dir)
	cs := NewContractStore(&ContractStoreConf{StoragePath: dir}, &mockRR{})
	err := cs.Init()
	assert.NoError(err)

	cs.(*contractStore).db.Put(fmt.Sprintf("%s/%s", ldbContractAddressPrefix, "abcd"), []byte(`!bad json{`))
	_, err = cs.ListContracts()
	assert.Regexp("FFEC100223", err)

	cs.(*contractStore).db.Put(fmt.Sprintf("%s/%s", ldbABIIDPrefix, "abcd"), []byte(`!bad json{`))
	_, err = cs.ListABIs()
	assert.Regexp("FFEC100223", err)

}
