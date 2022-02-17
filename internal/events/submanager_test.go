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

package events

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"testing"
	"time"

	"github.com/hyperledger/firefly-ethconnect/internal/kvstore"
	"github.com/hyperledger/firefly-ethconnect/mocks/contractregistrymocks"
	"github.com/hyperledger/firefly-ethconnect/mocks/ethmocks"
	"github.com/julienschmidt/httprouter"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockWebSocket struct {
	capturedNamespace string
	sender            chan interface{}
	broadcast         chan interface{}
	receiver          chan error
}

func (m *mockWebSocket) GetChannels(namespace string) (chan<- interface{}, chan<- interface{}, <-chan error) {
	m.capturedNamespace = namespace
	return m.sender, m.broadcast, m.receiver
}

func (m *mockWebSocket) SendReply(message interface{}) {}

func tempdir(t *testing.T) string {
	dir, _ := ioutil.TempDir("", "fly")
	t.Logf("tmpdir/create: %s", dir)
	return dir
}

func cleanup(t *testing.T, dir string) {
	t.Logf("tmpdir/cleanup: %s [dir]", dir)
	os.RemoveAll(dir)
}

func newMockWebSocket() *mockWebSocket {
	return &mockWebSocket{
		sender:    make(chan interface{}),
		broadcast: make(chan interface{}),
		receiver:  make(chan error, 1),
	}
}

func newTestSubscriptionManager() *subscriptionMGR {
	smconf := &SubscriptionManagerConf{}
	rpc := &ethmocks.RPCClient{}
	cr := &contractregistrymocks.ContractStore{}
	sm := NewSubscriptionManager(smconf, rpc, cr, newMockWebSocket()).(*subscriptionMGR)
	sm.db = kvstore.NewMockKV(nil)
	sm.config().WebhooksAllowPrivateIPs = true
	sm.config().EventPollingIntervalSec = 0
	return sm
}

func TestNestSubscriptionManagerBlockGapValidation(t *testing.T) {
	smconf := &SubscriptionManagerConf{
		CatchupModeBlockGap: 10,
		CatchupModePageSize: 1000,
	}
	rpc := &ethmocks.RPCClient{}
	cr := &contractregistrymocks.ContractStore{}
	sm := NewSubscriptionManager(smconf, rpc, cr, newMockWebSocket()).(*subscriptionMGR)
	assert.Equal(t, int64(1000), sm.conf.CatchupModeBlockGap)
}

func TestCobraInitSubscriptionManager(t *testing.T) {
	assert := assert.New(t)
	cmd := cobra.Command{}
	conf := &SubscriptionManagerConf{}
	CobraInitSubscriptionManager(&cmd, conf)
	assert.NotNil(cmd.Flag("events-db"))
}

func TestInitLevelDBSuccess(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	router := &httprouter.Router{}
	svr := httptest.NewServer(router)
	defer svr.Close()

	sm := newTestSubscriptionManager()
	sm.config().EventLevelDBPath = path.Join(dir, "db")
	err := sm.Init()
	assert.Equal(nil, err)
	sm.Close(true)
}

func TestInitLevelDBFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	ioutil.WriteFile(path.Join(dir, "db"), []byte("I am not a directory"), 0644)
	sm := newTestSubscriptionManager()
	sm.config().EventLevelDBPath = path.Join(dir, "db")
	err := sm.Init()
	assert.Regexp("not a directory", err.Error())
	sm.Close(true)
}

func TestActionAndSubscriptionLifecyle(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	subscriptionName := "testSub"
	defer cleanup(t, dir)

	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_blockNumber").Return(nil)
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newFilter", mock.Anything).Return(nil)
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterLogs", mock.Anything).Return(nil)
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_uninstallFilter", mock.Anything).Return(nil)

	sm := newTestSubscriptionManager()
	sm.rpc = rpc

	sm.db, _ = kvstore.NewLDBKeyValueStore(path.Join(dir, "db"))
	defer sm.db.Close()

	ctx := context.Background()
	assert.Equal([]*SubscriptionInfo{}, sm.Subscriptions(ctx))
	assert.Equal([]*StreamInfo{}, sm.Streams(ctx))

	stream, err := sm.AddStream(ctx, &StreamInfo{
		Type:    "webhook",
		Webhook: &webhookActionInfo{URL: "http://test.invalid"},
	})
	assert.NoError(err)

	sub, err := sm.AddSubscription(ctx, nil, nil, &ethbinding.ABIElementMarshaling{Name: "ping"}, stream.ID, "", subscriptionName)
	assert.NoError(err)
	assert.Equal(stream.ID, sub.Stream)

	assert.Equal([]*SubscriptionInfo{sub}, sm.Subscriptions(ctx))
	assert.Equal([]*StreamInfo{stream}, sm.Streams(ctx))

	retSub, _ := sm.SubscriptionByID(ctx, sub.ID)
	assert.Equal(sub, retSub)
	retStream, _ := sm.StreamByID(ctx, stream.ID)
	assert.Equal(stream, retStream)

	assert.Nil(sm.SubscriptionByID(ctx, stream.ID))
	assert.Nil(sm.StreamByID(ctx, sub.ID))

	err = sm.SuspendStream(ctx, stream.ID)
	assert.NoError(err)

	err = sm.SuspendStream(ctx, stream.ID)
	assert.NoError(err)

	for {
		// Incase the suspend takes a little time
		if err = sm.ResumeStream(ctx, stream.ID); err == nil {
			break
		} else {
			time.Sleep(1 * time.Millisecond)
		}
	}

	err = sm.ResumeStream(ctx, stream.ID)
	assert.Regexp("Event processor is already active. Suspending:false", err)

	// Reload
	sm.Close(false)
	mux := http.NewServeMux()
	svr := httptest.NewServer(mux)
	defer svr.Close()
	sm = newTestSubscriptionManager()
	sm.conf.EventLevelDBPath = path.Join(dir, "db")
	sm.rpc = rpc
	err = sm.Init()
	assert.NoError(err)

	assert.Equal(1, len(sm.streams))
	assert.Equal(1, len(sm.subscriptions))

	err = sm.ResetSubscription(ctx, sub.ID, "0")
	assert.NoError(err)

	err = sm.DeleteSubscription(ctx, sub.ID)
	assert.NoError(err)

	err = sm.DeleteStream(ctx, stream.ID)
	assert.NoError(err)

	sm.Close(true)
}

func TestActionChildCleanup(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager()

	blockCall := make(chan struct{})
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) { <-blockCall }).Return(nil)
	sm.rpc = rpc

	sm.db, _ = kvstore.NewLDBKeyValueStore(path.Join(dir, "db"))
	defer sm.db.Close()

	ctx := context.Background()
	stream, err := sm.AddStream(ctx, &StreamInfo{
		Type:    "webhook",
		Webhook: &webhookActionInfo{URL: "http://test.invalid"},
	})
	assert.NoError(err)

	sm.AddSubscription(ctx, nil, nil, &ethbinding.ABIElementMarshaling{Name: "ping"}, stream.ID, "12345", "")
	err = sm.DeleteStream(ctx, stream.ID)
	assert.NoError(err)

	assert.Equal([]*SubscriptionInfo{}, sm.Subscriptions(ctx))
	assert.Equal([]*StreamInfo{}, sm.Streams(ctx))

	close(blockCall)
	sm.Close(true)
}

func TestStreamAndSubscriptionErrors(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	subscriptionName := "testSub"
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager()

	blockCall := make(chan struct{})
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) { <-blockCall }).Return(nil)
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_newFilter", mock.Anything).Return(nil).Maybe()
	sm.rpc = rpc

	sm.db, _ = kvstore.NewLDBKeyValueStore(path.Join(dir, "db"))
	defer sm.db.Close()

	ctx := context.Background()
	assert.Equal([]*SubscriptionInfo{}, sm.Subscriptions(ctx))
	assert.Equal([]*StreamInfo{}, sm.Streams(ctx))

	stream, err := sm.AddStream(ctx, &StreamInfo{
		Type:    "webhook",
		Webhook: &webhookActionInfo{URL: "http://test.invalid"},
	})
	assert.NoError(err)

	sub, err := sm.AddSubscriptionDirect(ctx, &SubscriptionCreateDTO{
		Name:   subscriptionName,
		Stream: stream.ID,
		Event:  &ethbinding.ABIElementMarshaling{Name: "ping"},
	})
	assert.NoError(err)

	err = sm.ResetSubscription(ctx, sub.ID, "badness")
	assert.Regexp("FromBlock cannot be parsed as a BigInt", err)

	sm.db.Close()
	err = sm.ResetSubscription(ctx, sub.ID, "0")
	assert.Regexp("Failed to store subscription: leveldb: closed", err)

	close(blockCall)
	sm.Close(true)
}

func TestStreamAndSubscriptionInlineMethodArray(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	subscriptionName := "testSub"
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager()

	blockCall := make(chan struct{})
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) { <-blockCall }).Return(nil)
	sm.rpc = rpc

	sm.db, _ = kvstore.NewLDBKeyValueStore(path.Join(dir, "db"))
	defer sm.db.Close()

	ctx := context.Background()
	assert.Equal([]*SubscriptionInfo{}, sm.Subscriptions(ctx))
	assert.Equal([]*StreamInfo{}, sm.Streams(ctx))

	stream, err := sm.AddStream(ctx, &StreamInfo{
		Type:    "webhook",
		Webhook: &webhookActionInfo{URL: "http://test.invalid"},
	})
	assert.NoError(err)

	sub, err := sm.AddSubscriptionDirect(ctx, &SubscriptionCreateDTO{
		Name:   subscriptionName,
		Stream: stream.ID,
		Event:  &ethbinding.ABIElementMarshaling{Name: "ping"},
		Methods: ethbinding.ABIMarshaling{
			{
				Type: "function",
				Name: "doPing",
			},
		},
	})
	assert.NoError(err)

	assert.NotNil(sub.ABI.Inline)
	assert.Equal("doPing", sub.ABI.Inline[0].Name)

	close(blockCall)
	sm.Close(true)
}

func TestResetSubscriptionErrors(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager()

	rpc := &ethmocks.RPCClient{}
	sm.rpc = rpc
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_uninstallFilter", mock.Anything).Return(nil)

	sm.db = kvstore.NewMockKV(fmt.Errorf("pop"))
	defer sm.db.Close()

	ctx := context.Background()
	_, err := sm.AddStream(ctx, &StreamInfo{Type: "random"})
	assert.Regexp("Unknown action type 'random'", err)
	_, err = sm.AddStream(ctx, &StreamInfo{
		Type:    "webhook",
		Webhook: &webhookActionInfo{URL: "http://test.invalid"},
	})
	assert.Regexp("Failed to store stream: pop", err)
	sm.streams["teststream"] = newTestStream()
	err = sm.DeleteStream(ctx, "nope")
	assert.Regexp("Stream with ID 'nope' not found", err)
	err = sm.SuspendStream(ctx, "nope")
	assert.Regexp("Stream with ID 'nope' not found", err)
	err = sm.ResumeStream(ctx, "nope")
	assert.Regexp("Stream with ID 'nope' not found", err)
	err = sm.DeleteStream(ctx, "teststream")
	assert.Regexp("pop", err)

	_, err = sm.AddSubscription(ctx, nil, nil, &ethbinding.ABIElementMarshaling{Name: "any"}, "nope", "", "")
	assert.Regexp("Stream with ID 'nope' not found", err)
	_, err = sm.AddSubscription(ctx, nil, nil, &ethbinding.ABIElementMarshaling{Name: "any"}, "teststream", "", "test")
	assert.Regexp("Failed to store subscription: pop", err)
	_, err = sm.AddSubscription(ctx, nil, nil, &ethbinding.ABIElementMarshaling{Name: "any"}, "teststream", "!bad integer", "")
	assert.Regexp("FromBlock cannot be parsed as a BigInt", err)
	sm.subscriptions["testsub"] = &subscription{info: &SubscriptionInfo{}, rpc: sm.rpc}
	err = sm.ResetSubscription(ctx, "nope", "0")
	assert.Regexp("Subscription with ID 'nope' not found", err)
	err = sm.DeleteSubscription(ctx, "nope")
	assert.Regexp("Subscription with ID 'nope' not found", err)
	err = sm.DeleteSubscription(ctx, "testsub")
	assert.Regexp("pop", err)
}

func TestRecoverErrors(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager()
	sm.db, _ = kvstore.NewLDBKeyValueStore(path.Join(dir, "db"))
	defer sm.db.Close()

	sm.db.Put(streamIDPrefix+"esid1", []byte(":bad json"))
	sm.db.Put(streamIDPrefix+"esid2", []byte("{}"))
	sm.db.Put(subIDPrefix+"subid1", []byte(":bad json"))
	sm.db.Put(subIDPrefix+"subid2", []byte("{}"))

	sm.recoverStreams()
	sm.recoverSubscriptions()

	assert.Equal(0, len(sm.streams))
	assert.Equal(0, len(sm.subscriptions))
}
