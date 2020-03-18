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

package kldevents

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"reflect"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldkvstore"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

type mockRPC struct {
	capturedMethod string
	mockError      error
	result         interface{}
}

func (m *mockRPC) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	m.capturedMethod = method
	v := reflect.ValueOf(result)
	v.Elem().Set(reflect.ValueOf(m.result))
	return m.mockError
}

func tempdir(t *testing.T) string {
	dir, _ := ioutil.TempDir("", "kld")
	t.Logf("tmpdir/create: %s", dir)
	return dir
}

func cleanup(t *testing.T, dir string) {
	t.Logf("tmpdir/cleanup: %s [dir]", dir)
	os.RemoveAll(dir)
}

func newTestSubscriptionManager() *subscriptionMGR {
	smconf := &SubscriptionManagerConf{}
	sm := NewSubscriptionManager(smconf, nil).(*subscriptionMGR)
	sm.rpc = kldeth.NewMockRPCClientForSync(nil, nil)
	sm.db = kldkvstore.NewMockKV(nil)
	sm.config().WebhooksAllowPrivateIPs = true
	sm.config().EventPollingIntervalSec = 0
	return sm
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
	sm.Close()
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
	sm.Close()
}

func TestActionAndSubscriptionLifecyle(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager()
	sm.rpc = kldeth.NewMockRPCClientForSync(nil, nil)
	sm.db, _ = kldkvstore.NewLDBKeyValueStore(path.Join(dir, "db"))
	defer sm.db.Close()

	ctx := context.Background()
	assert.Equal([]*SubscriptionInfo{}, sm.Subscriptions(ctx))
	assert.Equal([]*StreamInfo{}, sm.Streams(ctx))

	stream, err := sm.AddStream(ctx, &StreamInfo{
		Type:    "webhook",
		Webhook: &webhookAction{URL: "http://test.invalid"},
	})
	assert.NoError(err)

	sub, err := sm.AddSubscription(ctx, nil, &kldbind.ABIEvent{Name: "ping"}, stream.ID, "")
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
	assert.EqualError(err, "Event processor is already active. Suspending:false")

	// Reload
	sm.Close()
	mux := http.NewServeMux()
	svr := httptest.NewServer(mux)
	defer svr.Close()
	sm = newTestSubscriptionManager()
	sm.conf.EventLevelDBPath = path.Join(dir, "db")
	sm.rpcConf = &kldeth.RPCConnOpts{URL: svr.URL}
	err = sm.Init()
	assert.NoError(err)

	assert.Equal(1, len(sm.streams))
	assert.Equal(1, len(sm.subscriptions))

	err = sm.DeleteSubscription(ctx, sub.ID)
	assert.NoError(err)

	err = sm.DeleteStream(ctx, stream.ID)
	assert.NoError(err)

	sm.Close()
}

func TestActionChildCleanup(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager()
	sm.rpc = kldeth.NewMockRPCClientForSync(nil, nil)
	sm.db, _ = kldkvstore.NewLDBKeyValueStore(path.Join(dir, "db"))
	defer sm.db.Close()

	ctx := context.Background()
	stream, err := sm.AddStream(ctx, &StreamInfo{
		Type:    "webhook",
		Webhook: &webhookAction{URL: "http://test.invalid"},
	})
	assert.NoError(err)

	_, err = sm.AddSubscription(ctx, nil, &kldbind.ABIEvent{Name: "ping"}, stream.ID, "12345")
	err = sm.DeleteStream(ctx, stream.ID)
	assert.NoError(err)

	assert.Equal([]*SubscriptionInfo{}, sm.Subscriptions(ctx))
	assert.Equal([]*StreamInfo{}, sm.Streams(ctx))

	sm.Close()
}
func TestStreamAndSubscriptionErrors(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager()
	sm.rpc = kldeth.NewMockRPCClientForSync(nil, nil)
	sm.db = kldkvstore.NewMockKV(fmt.Errorf("pop"))
	defer sm.db.Close()

	ctx := context.Background()
	_, err := sm.AddStream(ctx, &StreamInfo{Type: "random"})
	assert.EqualError(err, "Unknown action type 'random'")
	_, err = sm.AddStream(ctx, &StreamInfo{
		Type:    "webhook",
		Webhook: &webhookAction{URL: "http://test.invalid"},
	})
	assert.EqualError(err, "Failed to store stream: pop")
	sm.streams["teststream"] = newTestStream()
	err = sm.DeleteStream(ctx, "nope")
	assert.EqualError(err, "Stream with ID 'nope' not found")
	err = sm.SuspendStream(ctx, "nope")
	assert.EqualError(err, "Stream with ID 'nope' not found")
	err = sm.ResumeStream(ctx, "nope")
	assert.EqualError(err, "Stream with ID 'nope' not found")
	err = sm.DeleteStream(ctx, "teststream")
	assert.EqualError(err, "pop")

	_, err = sm.AddSubscription(ctx, nil, &kldbind.ABIEvent{Name: "any"}, "nope", "")
	assert.EqualError(err, "Stream with ID 'nope' not found")
	_, err = sm.AddSubscription(ctx, nil, &kldbind.ABIEvent{Name: "any"}, "teststream", "")
	assert.EqualError(err, "Failed to store subscription: pop")
	_, err = sm.AddSubscription(ctx, nil, &kldbind.ABIEvent{Name: "any"}, "teststream", "!bad integer")
	assert.EqualError(err, "FromBlock cannot be parsed as a BigInt")
	sm.subscriptions["testsub"] = &subscription{info: &SubscriptionInfo{}, rpc: sm.rpc}
	err = sm.DeleteSubscription(ctx, "nope")
	assert.EqualError(err, "Subscription with ID 'nope' not found")
	err = sm.DeleteSubscription(ctx, "testsub")
	assert.EqualError(err, "pop")
}

func TestRecoverErrors(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager()
	sm.db, _ = kldkvstore.NewLDBKeyValueStore(path.Join(dir, "db"))
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
