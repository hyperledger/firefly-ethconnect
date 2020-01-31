// Copyright 2018, 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldtx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestNewAddressBookDefaultPropNames(t *testing.T) {
	assert := assert.New(t)

	a := NewAddressBook(&AddressBookConf{
		AddressbookURLPrefix: "http://localhost:12345/",
	}, &kldeth.RPCConf{})
	ab := a.(*addressBook)
	assert.Equal("http://localhost:12345/", ab.conf.AddressbookURLPrefix)
	assert.Equal(defaultRPCEndpointProp, ab.conf.PropNames.RPCEndpoint)
}

func TestNewAddressBookCustomPropNames(t *testing.T) {
	assert := assert.New(t)

	a := NewAddressBook(&AddressBookConf{
		AddressbookURLPrefix: "http://localhost:12345",
		PropNames: AddressBookPropNamesConf{
			RPCEndpoint: "rpcEndpointProp",
		},
	}, &kldeth.RPCConf{})
	ab := a.(*addressBook)
	assert.Equal("http://localhost:12345/", ab.conf.AddressbookURLPrefix)
	assert.Equal("rpcEndpointProp", ab.conf.PropNames.RPCEndpoint)
}

func TestLookupWithCaching(t *testing.T) {
	assert := assert.New(t)

	var serverURL string
	var failRPC = false
	router := &httprouter.Router{}
	router.POST("/", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		if failRPC {
			res.WriteHeader(500)
			return
		}
		res.WriteHeader(200)
		res.Write([]byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"2109240103\"}"))
	})
	router.GET("/addresses/:address", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(200)
		res.Write([]byte("{\"rpcEndpointProp\":\"" + serverURL + "\"}"))
	})
	server := httptest.NewServer(router)
	serverURL = server.URL
	defer server.Close()

	a := NewAddressBook(&AddressBookConf{
		AddressbookURLPrefix: serverURL + "/addresses",
		PropNames: AddressBookPropNamesConf{
			RPCEndpoint: "rpcEndpointProp",
		},
	}, &kldeth.RPCConf{})
	ab := a.(*addressBook)

	log.SetLevel(log.DebugLevel)
	ctx := context.Background()
	rpc, err := ab.lookup(ctx, "0xdb0997dccd71607bd6ee378723a12ef8478e4ed6")

	assert.NoError(err)
	assert.NotNil(rpc)

	rpcCachedSameHost, err := ab.lookup(ctx, "0x125b194949a37d7ea6e2bac5bd21097d37a36974")

	assert.NoError(err)
	assert.Equal(rpc, rpcCachedSameHost)

	rpcCachedSameAddr, err := ab.lookup(ctx, "0x125b194949a37d7ea6e2bac5bd21097d37a36974")

	assert.NoError(err)
	assert.Equal(rpc, rpcCachedSameAddr)

	failRPC = true

	rpcNewAfterFailure, err := ab.lookup(ctx, "0x125b194949a37d7ea6e2bac5bd21097d37a36974")

	assert.NoError(err)
	assert.NotEqual(rpc, rpcNewAfterFailure)

}

func TestLookupBadURL(t *testing.T) {
	assert := assert.New(t)

	var serverURL string
	router := &httprouter.Router{}
	router.GET("/addresses/:address", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(200)
		res.Write([]byte("{\"rpcEndpointProp\":\"://\"}"))
	})
	server := httptest.NewServer(router)
	serverURL = server.URL
	defer server.Close()

	a := NewAddressBook(&AddressBookConf{
		AddressbookURLPrefix: serverURL + "/addresses",
		PropNames: AddressBookPropNamesConf{
			RPCEndpoint: "rpcEndpointProp",
		},
	}, &kldeth.RPCConf{})
	ab := a.(*addressBook)

	ctx := context.Background()
	_, err := ab.lookup(ctx, "0xdb0997dccd71607bd6ee378723a12ef8478e4ed6")

	assert.EqualError(err, "Invalid URL obtained for address")
}

func TestLookupFallbackAddress(t *testing.T) {
	assert := assert.New(t)

	var serverURL string
	router := &httprouter.Router{}
	router.GET("/addresses/:address", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(404)
	})
	server := httptest.NewServer(router)
	serverURL = server.URL
	defer server.Close()

	a := NewAddressBook(&AddressBookConf{
		AddressbookURLPrefix: serverURL + "/addresses",
		PropNames: AddressBookPropNamesConf{
			RPCEndpoint: "rpcEndpointProp",
		},
	}, &kldeth.RPCConf{
		RPC: kldeth.RPCConnOpts{
			URL: serverURL,
		},
	})
	ab := a.(*addressBook)

	log.SetLevel(log.DebugLevel)
	ctx := context.Background()
	rpc, err := ab.lookup(ctx, "0xdb0997dccd71607bd6ee378723a12ef8478e4ed6")

	assert.NoError(err)
	assert.NotNil(rpc)

}

func TestLookupNoFallbackAddress(t *testing.T) {
	assert := assert.New(t)

	var serverURL string
	router := &httprouter.Router{}
	router.GET("/addresses/:address", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(404)
	})
	server := httptest.NewServer(router)
	serverURL = server.URL
	defer server.Close()

	a := NewAddressBook(&AddressBookConf{
		AddressbookURLPrefix: serverURL + "/addresses",
		PropNames: AddressBookPropNamesConf{
			RPCEndpoint: "rpcEndpointProp",
		},
	}, &kldeth.RPCConf{})
	ab := a.(*addressBook)

	log.SetLevel(log.DebugLevel)
	ctx := context.Background()
	_, err := ab.lookup(ctx, "0xdb0997dccd71607bd6ee378723a12ef8478e4ed6")

	assert.EqualError(err, "Unknown address")

}

func TestLookupBadResponse(t *testing.T) {
	assert := assert.New(t)

	var serverURL string
	router := &httprouter.Router{}
	router.GET("/addresses/:address", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(200)
		res.Write([]byte("{}"))
	})
	server := httptest.NewServer(router)
	serverURL = server.URL
	defer server.Close()

	a := NewAddressBook(&AddressBookConf{
		AddressbookURLPrefix: serverURL + "/addresses",
		PropNames: AddressBookPropNamesConf{
			RPCEndpoint: "rpcEndpointProp",
		},
	}, &kldeth.RPCConf{})
	ab := a.(*addressBook)

	log.SetLevel(log.DebugLevel)
	ctx := context.Background()
	_, err := ab.lookup(ctx, "0xdb0997dccd71607bd6ee378723a12ef8478e4ed6")

	assert.EqualError(err, "'rpcEndpointProp' missing in Addressbook response")

}

func TestLookupFailureResponse(t *testing.T) {
	assert := assert.New(t)

	var serverURL string
	router := &httprouter.Router{}
	router.GET("/addresses/:address", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(500)
	})
	server := httptest.NewServer(router)
	serverURL = server.URL
	defer server.Close()

	a := NewAddressBook(&AddressBookConf{
		AddressbookURLPrefix: serverURL + "/addresses",
		PropNames: AddressBookPropNamesConf{
			RPCEndpoint: "rpcEndpointProp",
		},
	}, &kldeth.RPCConf{})
	ab := a.(*addressBook)

	log.SetLevel(log.DebugLevel)
	_, err := ab.lookup(context.Background(), "0xdb0997dccd71607bd6ee378723a12ef8478e4ed6")

	assert.EqualError(err, "Could not process Addressbook [500] response")

}

func TestResolveHostOK(t *testing.T) {
	assert := assert.New(t)

	a := NewAddressBook(&AddressBookConf{
		HostsFile: "../../test/hosts_example",
	}, &kldeth.RPCConf{})
	ab := a.(*addressBook)

	url, err := ab.resolveHost("http://foo.bar:12345")

	assert.NoError(err)
	assert.Equal("http://127.0.0.2:12345", url.String())

}

func TestResolveHostMissing(t *testing.T) {
	assert := assert.New(t)

	a := NewAddressBook(&AddressBookConf{
		HostsFile: "../../test/hosts_example",
	}, &kldeth.RPCConf{})
	ab := a.(*addressBook)

	url, err := ab.resolveHost("http://another")

	assert.NoError(err)
	assert.Equal("http://another", url.String())

}

func TestResolveHostBadFile(t *testing.T) {
	assert := assert.New(t)

	a := NewAddressBook(&AddressBookConf{
		HostsFile: "badhostsfile",
	}, &kldeth.RPCConf{})
	ab := a.(*addressBook)

	_, err := ab.resolveHost("http://any")

	assert.NotNil(err)

}
