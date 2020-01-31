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

package kldeth

import (
	"context"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/julienschmidt/httprouter"
	"github.com/spf13/cobra"

	"github.com/kaleido-io/ethconnect/internal/kldauth"
	"github.com/kaleido-io/ethconnect/internal/kldauth/kldauthtest"
	"github.com/stretchr/testify/assert"
)

// mockEthClient is used internally within this class for testing the wrapping layer itself, rather than
// for mocking RPC calls
type mockEthClient struct{}

func (w *mockEthClient) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	return nil
}
func (w *mockEthClient) Subscribe(ctx context.Context, namespace string, channel interface{}, args ...interface{}) (*rpc.ClientSubscription, error) {
	return nil, nil
}
func (w *mockEthClient) Close() {}

func TestRPCWrapperConformsToInterface(t *testing.T) {
	// This test doesn't verify any interesting function, simply that the rpcWrapper
	// type mapper can be invoked without panics
	w := &rpcWrapper{rpc: &mockEthClient{}}
	var rpc RPCClientAll
	rpc = w
	rpc.Subscribe(context.Background(), "", nil)
	rpc.CallContext(context.Background(), nil, "")
	rpc.Close()
}

func TestCobraInitTxnProcessor(t *testing.T) {
	assert := assert.New(t)
	rconf := &RPCConf{}
	cmd := &cobra.Command{}
	CobraInitRPC(cmd, rconf)
	cmd.ParseFlags([]string{
		"-r", "http://localhost:8545",
	})
	assert.Equal("http://localhost:8545", rconf.RPC.URL)
}

func TestRPCConnectOK(t *testing.T) {
	assert := assert.New(t)
	router := &httprouter.Router{}
	testSvr := httptest.NewServer(router)
	defer testSvr.Close()

	rpc, err := RPCConnect(&RPCConnOpts{URL: testSvr.URL})
	assert.NoError(err)
	assert.NotNil(rpc)
}

func TestRPCConnectAuthOK(t *testing.T) {
	assert := assert.New(t)
	router := &httprouter.Router{}
	testSvr := httptest.NewServer(router)
	defer testSvr.Close()

	u, _ := url.Parse(testSvr.URL)
	u.User = url.UserPassword("user", "pass")
	rpc, err := RPCConnect(&RPCConnOpts{URL: u.String()})
	assert.NoError(err)
	assert.NotNil(rpc)
}

func TestRPCConnectFail(t *testing.T) {
	assert := assert.New(t)

	_, err := RPCConnect(&RPCConnOpts{URL: ""})
	assert.Error(err)
}

func TestSubscribeWrapper(t *testing.T) {
	assert := assert.New(t)

	mockRPC := NewMockRPCClientForAsync(nil)
	var iRPC RPCClientAsync
	iRPC = mockRPC
	c := make(chan interface{})
	sub, err := iRPC.Subscribe(context.Background(), "testns", c, "arg1")
	assert.NoError(err)
	assert.Equal("testns", mockRPC.SubResult.Namespace)
	assert.Equal([]interface{}{"arg1"}, mockRPC.SubResult.Args)
	assert.True(mockRPC.SubResult.Subscribed)
	assert.NotNil(sub.Err())
	sub.Unsubscribe()
	assert.False(mockRPC.SubResult.Subscribed)
	iRPC.(RPCClosable).Close()
	assert.True(mockRPC.Closed)
}

func TestCallContextWrapper(t *testing.T) {
	assert := assert.New(t)

	mockRPC := NewMockRPCClientForSync(nil, func(method string, res interface{}, args ...interface{}) {
		*(res.(*string)) = "mock result"
	})
	var iRPC RPCClient
	iRPC = mockRPC
	var strRetval string
	err := iRPC.CallContext(context.Background(), &strRetval, "rpcmethod1", "arg1", "arg2")
	assert.NoError(err)
	assert.Equal("mock result", strRetval)
	assert.Equal("rpcmethod1", mockRPC.MethodCapture)
	assert.Equal([]interface{}{"arg1", "arg2"}, mockRPC.ArgsCapture)
}

func TestCallContextWrapperAuth(t *testing.T) {
	assert := assert.New(t)

	kldauth.RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	w := &rpcWrapper{rpc: &mockEthClient{}}
	_, err := w.Subscribe(context.Background(), "", nil)
	assert.EqualError(err, "Unauthorized")
	err = w.CallContext(context.Background(), nil, "")
	assert.EqualError(err, "Unauthorized")

	kldauth.RegisterSecurityModule(nil)
}
