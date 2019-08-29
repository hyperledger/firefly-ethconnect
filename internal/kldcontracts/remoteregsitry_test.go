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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/stretchr/testify/assert"
)

func TestNewRemoteRegistryDefaultPropNames(t *testing.T) {
	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix:  "http://www.example1.com/",
		InstanceURLPrefix: "http://www.example2.com/",
	})
	rr := r.(*remoteRegistry)
	assert.Equal("http://www.example1.com/", rr.conf.FactoryURLPrefix)
	assert.Equal("http://www.example2.com/", rr.conf.InstanceURLPrefix)
	assert.Equal(defaultABIProp, rr.conf.PropNames.ABI)
	assert.Equal(defaultBytecodeProp, rr.conf.PropNames.Bytecode)
	assert.Equal(defaultDevdocProp, rr.conf.PropNames.Devdoc)
	assert.Equal(defaultDeployableProp, rr.conf.PropNames.Deployable)
}

func TestNewRemoteRegistryCustomPropNames(t *testing.T) {
	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix:  "http://www.example1.com",
		InstanceURLPrefix: "http://www.example2.com",
		PropNames: RemoteRegistryPropNamesConf{
			ABI:        "abiProp",
			Bytecode:   "bytecodeProp",
			Devdoc:     "devdocsProp",
			Deployable: "deployableProp",
		},
	})
	rr := r.(*remoteRegistry)
	assert.Equal("http://www.example1.com/", rr.conf.FactoryURLPrefix)
	assert.Equal("http://www.example2.com/", rr.conf.InstanceURLPrefix)
	assert.Equal("abiProp", rr.conf.PropNames.ABI)
	assert.Equal("bytecodeProp", rr.conf.PropNames.Bytecode)
	assert.Equal("devdocsProp", rr.conf.PropNames.Devdoc)
	assert.Equal("deployableProp", rr.conf.PropNames.Deployable)
}

func TestRemoteRegistryDoRequestBadURL(t *testing.T) {
	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{})
	rr := r.(*remoteRegistry)

	_, err := rr.doRequest("GET", "! a URL")
	assert.EqualError(err, "Error querying contract registry")
}

func TestRemoteRegistryLoadFactoryByIDSuccess(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		testDataBytes, _ := ioutil.ReadFile("../../test/simpleevents.solc.output.json")
		res.WriteHeader(200)
		res.Write(testDataBytes)
	})
	server := httptest.NewServer(router)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	res, err := rr.loadFactoryByID("testid")
	assert.NoError(err)
	assert.NotEmpty(res.Compiled)
	assert.Equal("set", res.ABI.Methods["set"].Name)
	assert.Contains(res.DevDoc, "set")
}

func TestRemoteRegistryLoadFactoryMissingABI(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{

    }`))
	})
	server := httptest.NewServer(router)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryByID("testid")
	assert.EqualError(err, "'abi' missing in contract registry response")
}

func TestRemoteRegistryLoadFactoryBadABIJSON(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{
      "abi": "!JSON"
    }`))
	})
	server := httptest.NewServer(router)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryByID("testid")
	assert.EqualError(err, "Error processing contract registry response")
}

func TestRemoteRegistryLoadFactoryMissingDevDoc(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{
      "abi": "[]"
    }`))
	})
	server := httptest.NewServer(router)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryByID("testid")
	assert.EqualError(err, "'devdoc' missing in contract registry response")
}

func TestRemoteRegistryLoadFactoryBadDevDoc(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{
      "abi": "[]",
      "devdoc": null
    }`))
	})
	server := httptest.NewServer(router)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryByID("testid")
	assert.EqualError(err, "'devdoc' not a string in contract registry response")
}

func TestRemoteRegistryLoadFactoryEmptyBytecode(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{
      "abi": "[]",
      "devdoc": "",
      "bin": ""
    }`))
	})
	server := httptest.NewServer(router)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryByID("testid")
	assert.EqualError(err, "'bin' empty in contract registry response")
}

func TestRemoteRegistryLoadFactoryBadBytecode(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(200)
		res.Write([]byte(`{
      "abi": "[]",
      "devdoc": "",
      "bin": "!HEX"
    }`))
	})
	server := httptest.NewServer(router)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryByID("testid")
	assert.EqualError(err, "Error processing contract registry response")
}

func TestRemoteRegistryLoadFactoryErrorStatus(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(500)
	})
	server := httptest.NewServer(router)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryByID("testid")
	assert.EqualError(err, "Error querying contract registry")
}

func TestRemoteRegistryLoadFactoryNotFound(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.WriteHeader(404)
	})
	server := httptest.NewServer(router)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	res, err := rr.loadFactoryByID("testid")
	assert.NoError(err)
	assert.Nil(res)
}

func TestRemoteRegistryLoadFactoryBadBody(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/somepath/:id", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		assert.Equal("testid", parms.ByName("id"))
		res.Write([]byte("!JSON"))
		res.WriteHeader(200)
	})
	server := httptest.NewServer(router)

	r := NewRemoteRegistry(&RemoteRegistryConf{
		FactoryURLPrefix: server.URL + "/somepath",
		PropNames: RemoteRegistryPropNamesConf{
			Bytecode: "bin",
		},
	})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryByID("testid")
	assert.EqualError(err, "Error processing contract registry response")
}

func TestRemoteRegistryLoadFactoryNOOP(t *testing.T) {
	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{})
	rr := r.(*remoteRegistry)

	res, err := rr.loadFactoryByID("testid")
	assert.NoError(err)
	assert.Nil(res)
}

func TestRemoteRegistryLoadFactoryByAddressStub(t *testing.T) {
	assert := assert.New(t)

	r := NewRemoteRegistry(&RemoteRegistryConf{})
	rr := r.(*remoteRegistry)

	_, err := rr.loadFactoryByAddress("testid")
	assert.EqualError(err, "Not implemented")
}
