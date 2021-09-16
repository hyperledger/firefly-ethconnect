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

package eth

import (
	"context"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/spf13/cobra"

	"github.com/hyperledger-labs/firefly-ethconnect/internal/auth"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/auth/authtest"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	"github.com/stretchr/testify/assert"
)

// mockEthClient is used internally within this class for testing the wrapping layer itself, rather than
// for mocking RPC calls
type mockEthClient struct{}

func (w *mockEthClient) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	return nil
}
func (w *mockEthClient) Subscribe(ctx context.Context, namespace string, channel interface{}, args ...interface{}) (*ethbinding.ClientSubscription, error) {
	return nil, nil
}
func (w *mockEthClient) Close() {}

// mockRPCSubscription is used internally within this class for testing the wrapping layer itself
type mockRPCSubscription struct {
	Namespace  string
	Args       []interface{}
	ErrChan    chan error
	MsgChan    chan<- interface{}
	Subscribed bool
}

// Err returns the configured mock error channel
func (ms *mockRPCSubscription) Err() <-chan error {
	return ms.ErrChan
}

// Unsubscribe captures the unsubscribe call
func (ms *mockRPCSubscription) Unsubscribe() {
	ms.Subscribed = false
}

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
	ms := &mockRPCSubscription{
		ErrChan:    make(chan error),
		Subscribed: true,
	}
	w := &subWrapper{s: ms}
	assert.NotNil(w.Err())
	w.Unsubscribe()
	assert.Equal(false, ms.Subscribed)
}

func TestCallContextWrapperAuth(t *testing.T) {
	assert := assert.New(t)

	auth.RegisterSecurityModule(&authtest.TestSecurityModule{})

	w := &rpcWrapper{rpc: &mockEthClient{}}
	_, err := w.Subscribe(context.Background(), "", nil)
	assert.EqualError(err, "Unauthorized")
	err = w.CallContext(context.Background(), nil, "")
	assert.EqualError(err, "Unauthorized")

	auth.RegisterSecurityModule(nil)
}
