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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldkvstore"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/stretchr/testify/assert"
)

type mockRR struct {
	idCapture     string
	addrCapture   string
	lookupCapture string
	deployMsg     *deployContractWithAddress
	err           error
}

func (rr *mockRR) loadFactoryForGateway(id string) (*kldmessages.DeployContract, error) {
	rr.idCapture = id
	if rr.deployMsg == nil {
		return nil, rr.err
	}
	return &rr.deployMsg.DeployContract, rr.err
}
func (rr *mockRR) loadFactoryForInstance(id string) (*deployContractWithAddress, error) {
	rr.addrCapture = id
	return rr.deployMsg, rr.err
}
func (rr *mockRR) registerInstance(lookupStr, address string) error {
	rr.lookupCapture = lookupStr
	rr.addrCapture = address
	return rr.err
}
func (rr *mockRR) close()      {}
func (rr *mockRR) init() error { return nil }

func TestNewRemoteRegistryDefaultPropNames(t *testing.T) {
	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix:  "http://www.example1.com/",
		InstanceURLPrefix: "http://www.example2.com/",
	})
	rr := r.(*remoteRegistry)
	assert.Equal("http://www.example1.com/", rr.conf.GatewayURLPrefix)
	assert.Equal("http://www.example2.com/", rr.conf.InstanceURLPrefix)
	assert.Equal(defaultIDProp, rr.conf.PropNames.ID)
	assert.Equal(defaultNameProp, rr.conf.PropNames.Name)
	assert.Equal(defaultABIProp, rr.conf.PropNames.ABI)
	assert.Equal(defaultBytecodeProp, rr.conf.PropNames.Bytecode)
	assert.Equal(defaultDevdocProp, rr.conf.PropNames.Devdoc)
	assert.Equal(defaultDeployableProp, rr.conf.PropNames.Deployable)
	assert.Equal(defaultAddressProp, rr.conf.PropNames.Address)
}

func TestNewRemoteRegistryCustomPropNames(t *testing.T) {
	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix:  "http://www.example1.com",
		InstanceURLPrefix: "http://www.example2.com",
		PropNames: RemoteRegistryPropNamesConf{
			ID:         "idProp",
			Name:       "nameProp",
			ABI:        "abiProp",
			Bytecode:   "bytecodeProp",
			Devdoc:     "devdocsProp",
			Deployable: "deployableProp",
			Address:    "addressProp",
		},
	})
	rr := r.(*remoteRegistry)
	assert.Equal("http://www.example1.com/", rr.conf.GatewayURLPrefix)
	assert.Equal("http://www.example2.com/", rr.conf.InstanceURLPrefix)
	assert.Equal("idProp", rr.conf.PropNames.ID)
	assert.Equal("nameProp", rr.conf.PropNames.Name)
	assert.Equal("abiProp", rr.conf.PropNames.ABI)
	assert.Equal("bytecodeProp", rr.conf.PropNames.Bytecode)
	assert.Equal("devdocsProp", rr.conf.PropNames.Devdoc)
	assert.Equal("deployableProp", rr.conf.PropNames.Deployable)
	assert.Equal("addressProp", rr.conf.PropNames.Address)
}

func TestRemoteRegistryInitDB(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)

	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		CacheDB: path.Join(dir, "test"),
	})
	rr := r.(*remoteRegistry)

	err := rr.init()
	assert.NoError(err)
	rr.close()
}

func TestRemoteRegistryInitBadDB(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)

	assert := assert.New(t)

	db := path.Join(dir, "test")
	ioutil.WriteFile(db, []byte{}, 0644)
	r := NewRemoteRegistry(&RemoteRegistryConf{
		CacheDB: db,
	})
	rr := r.(*remoteRegistry)

	err := rr.init()
	assert.Regexp("Failed to initialize cache for remote registry", err.Error())
	rr.close()
}

func TestRemoteRegistryloadFactoryForGatewaySuccess(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		testDataBytes, _ := ioutil.ReadFile("../../test/simpleevents.solc.output.json")
		res.WriteHeader(200)
		res.Write(testDataBytes)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	res, err := rr.loadFactoryForGateway("testid")
	assert.NoError(err)
	assert.NotEmpty(res.Compiled)
	assert.Equal("set", res.ABI.Methods["set"].Name)
	assert.Contains(res.DevDoc, "set")
}

func TestRemoteRegistryloadFactoryForGatewayCached(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)

	assert := assert.New(t)

	firstCall := true
	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.True(firstCall)
		firstCall = false
		assert.Equal("testid", parms.ByName("id"))
		testDataBytes, _ := ioutil.ReadFile("../../test/simpleevents.solc.output.json")
		res.WriteHeader(200)
		res.Write(testDataBytes)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		CacheDB:          path.Join(dir, "testdb"),
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)
	rr.init()
	defer rr.close()

	res1, err := rr.loadFactoryForGateway("testid")
	assert.NoError(err)
	res2, err := rr.loadFactoryForGateway("testid")
	assert.NoError(err)
	assert.Equal(res1.Headers.ID, res2.Headers.ID)
	assert.Equal(res1.ABI, res2.ABI)
	assert.Equal(res1.DevDoc, res2.DevDoc)
	assert.Equal(res1.Compiled, res2.Compiled)
}

func TestRemoteRegistryRegisterInstanceSuccess(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.POST("/somepath", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		var bodyMap map[string]string
		json.NewDecoder(req.Body).Decode(&bodyMap)
		assert.Equal("testid", bodyMap["name"])
		assert.Equal("12345", bodyMap["address"])
		assert.Equal("application/json", req.Header.Get("content-type"))
		res.WriteHeader(204)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		InstanceURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	err := rr.registerInstance("testid", "12345")
	assert.NoError(err)
}

func TestRemoteRegistryRegisterInstanceFail(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.POST("/somepath", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(500)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		InstanceURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	err := rr.registerInstance("testid", "12345")
	assert.Regexp("Failed to register instance in remote registry", err)
}

func TestRemoteRegistryRegisterNoInstanceURL(t *testing.T) {
	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	err := rr.registerInstance("testid", "12345")
	assert.EqualError(err, "No remote registry is configured")
}

func TestRemoteRegistryLoadFactoryMissingID(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{

    }`))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryForGateway("testid")
	assert.EqualError(err, "'id' missing in Contract registry response")
}

func TestRemoteRegistryLoadFactoryMissingABI(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{
      "id": "12345"
    }`))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryForGateway("testid")
	assert.EqualError(err, "'abi' missing in Contract registry response")
}

func TestRemoteRegistryLoadFactoryBadABIJSON(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{
      "id": "12345",
      "abi": "!JSON"
    }`))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryForGateway("testid")
	assert.EqualError(err, "Error processing contract registry response")
}

func TestRemoteRegistryLoadFactoryMissingDevDoc(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{
      "id": "12345",
      "abi": "[]"
    }`))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryForGateway("testid")
	assert.EqualError(err, "'devdoc' missing in Contract registry response")
}

func TestRemoteRegistryLoadFactoryBadDevDoc(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{
      "id": "12345",
      "abi": "[]",
      "devdoc": 123
    }`))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryForGateway("testid")
	assert.EqualError(err, "'devdoc' not a string in Contract registry response")
}

func TestRemoteRegistryLoadFactoryEmptyBytecode(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{
      "id": "12345",
      "abi": "[]",
      "devdoc": null,
      "bin": ""
    }`))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryForGateway("testid")
	assert.EqualError(err, "'bin' empty (or null) in Contract registry response")
}

func TestRemoteRegistryLoadFactoryBadBytecode(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{
      "id": "12345",
      "abi": "[]",
      "devdoc": "",
      "bin": "!HEX"
    }`))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryForGateway("testid")
	assert.EqualError(err, "Error processing contract registry response")
}

func TestRemoteRegistryLoadFactoryErrorStatusGeneric(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(500)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryForGateway("testid")
	assert.EqualError(err, "Could not process Contract registry [500] response")
}

func TestRemoteRegistryLoadFactoryErrorStatus(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(500)
		res.Write([]byte("{\"errorMessage\":\"poof\"}"))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryForGateway("testid")
	assert.EqualError(err, "Contract registry returned [500]: poof")
}

func TestRemoteRegistryLoadFactoryNotFound(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(404)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	res, err := rr.loadFactoryForGateway("testid")
	assert.NoError(err)
	assert.Nil(res)
}

func TestRemoteRegistryLoadFactoryBadBody(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte("!JSON"))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		GatewayURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryForGateway("testid")
	assert.EqualError(err, "Could not process Contract registry [200] response")
}

func TestRemoteRegistryLoadFactoryNOOP(t *testing.T) {
	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{})
	rr := r.(*remoteRegistry)

	res, err := rr.loadFactoryForGateway("testid")
	assert.NoError(err)
	assert.Nil(res)
}

func TestRemoteRegistryloadFactoryForInstanceSuccess(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`
      {
        "address": "0x35344E187D669D930C9d513AaC63Ae204fC03C18",
        "id": "12345",
        "abi": "[]",
        "devdoc": "",
        "bin": "0x"
      }
    `))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	r := NewRemoteRegistry(&RemoteRegistryConf{
		InstanceURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	res, err := rr.loadFactoryForInstance("testid")
	assert.NoError(err)
	assert.Equal(res.Address, "35344e187d669d930c9d513aac63ae204fc03c18")
}

func TestRemoteRegistryLoadInstanceNOOP(t *testing.T) {
	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{})
	rr := r.(*remoteRegistry)

	res, err := rr.loadFactoryForInstance("testid")
	assert.NoError(err)
	assert.Nil(res)
}

func TestRemoteRegistryLoadFactoryFromCacheDBBadBytes(t *testing.T) {
	dir := tempdir()
	defer cleanup(dir)

	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		CacheDB: path.Join(dir, "testdb"),
	})
	rr := r.(*remoteRegistry)
	rr.init()
	defer rr.close()

	rr.db.Put("testid", []byte("!Bad JSON!"))

	msg := rr.loadFactoryFromCacheDB("testid")
	assert.Nil(msg)
}

func TestRemoteRegistryStoreFactoryToCacheDBBadObj(t *testing.T) {
	r := NewRemoteRegistry(&RemoteRegistryConf{})
	rr := r.(*remoteRegistry)
	mockKV := kldkvstore.NewMockKV(nil)
	rr.db = mockKV
	mockKV.StoreErr = fmt.Errorf("pop")
	rr.storeFactoryToCacheDB("testid", nil)
}
