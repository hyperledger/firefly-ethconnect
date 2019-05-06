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
	"net/http/httptest"
	"os"
	"path"
	"reflect"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
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

func newTestSubscriptionManager(dir string) *subscriptionMGR {
	smconf := &SubscriptionManagerConf{
		LevelDBPath: path.Join(dir, "db"),
	}
	rconf := &kldeth.RPCConnOpts{URL: ""}
	sm := NewSubscriptionManager(smconf, rconf).(*subscriptionMGR)
	sm.rpc = kldeth.NewMockRPCClientForSync(nil, nil)
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

	sm := newTestSubscriptionManager(dir)
	sm.rpcConf.URL = svr.URL
	err := sm.Init()
	assert.Equal(nil, err)
	sm.Close()
}

func TestInitLevelDBFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	ioutil.WriteFile(path.Join(dir, "db"), []byte("I am not a directory"), 0644)
	sm := newTestSubscriptionManager(dir)
	err := sm.Init()
	assert.Regexp("not a directory", err.Error())
	sm.Close()
}

func TestInitLevelRPCFail(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager(dir)
	err := sm.Init()
	assert.Regexp("missing address", err.Error())
	sm.Close()
}

func TestActionAndSubscriptionLifecyle(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager(dir)
	sm.rpc = kldeth.NewMockRPCClientForSync(nil, nil)
	sm.db, _ = newLDBKeyValueStore(path.Join(dir, "db"))
	defer sm.db.Close()

	assert.Equal([]*SubscriptionInfo{}, sm.Subscriptions())
	assert.Equal([]*ActionInfo{}, sm.Actions())

	action, err := sm.AddAction(&ActionInfo{
		Type:    "webhook",
		Webhook: &webhookAction{URL: "http://test.invalid"},
	})
	assert.NoError(err)

	sub, err := sm.AddSubscription(nil, &kldbind.ABIEvent{Name: "ping"}, action.ID)
	assert.NoError(err)
	assert.Equal(action.ID, sub.Action)

	assert.Equal([]*SubscriptionInfo{sub}, sm.Subscriptions())
	assert.Equal([]*ActionInfo{action}, sm.Actions())

	assert.Equal(sub, sm.SubscriptionByID(sub.ID))
	assert.Equal(action, sm.ActionByID(action.ID))

	assert.Nil(sm.SubscriptionByID(action.ID))
	assert.Nil(sm.ActionByID(sub.ID))

	err = sm.DeleteAction(action.ID)
	assert.EqualError(err, "The following subscriptions are still attached: "+sub.ID)

	err = sm.SuspendAction(action.ID)
	assert.NoError(err)

	err = sm.SuspendAction(action.ID)
	assert.NoError(err)

	for {
		// Incase the suspend takes a little time
		if err = sm.ResumeAction(action.ID); err == nil {
			break
		} else {
			time.Sleep(1 * time.Millisecond)
		}
	}

	err = sm.ResumeAction(action.ID)
	assert.EqualError(err, "Event processor is already active. Suspending:false")

	err = sm.DeleteSubscription(sub.ID)
	assert.NoError(err)

	err = sm.DeleteAction(action.ID)
	assert.NoError(err)

	sm.Close()
}

func TestActionAndSubscriptionErrors(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	sm := newTestSubscriptionManager(dir)
	sm.rpc = kldeth.NewMockRPCClientForSync(nil, nil)
	sm.db = newMockKV(fmt.Errorf("pop"))
	defer sm.db.Close()

	_, err := sm.AddAction(&ActionInfo{Type: "random"})
	assert.EqualError(err, "Unknown action type 'random'")
	_, err = sm.AddAction(&ActionInfo{
		Type:    "webhook",
		Webhook: &webhookAction{URL: "http://test.invalid"},
	})
	assert.EqualError(err, "Failed to store action: pop")
	sm.actions["testaction"] = newTestAction()
	err = sm.DeleteAction("nope")
	assert.EqualError(err, "Action with ID 'nope' not found")
	err = sm.SuspendAction("nope")
	assert.EqualError(err, "Action with ID 'nope' not found")
	err = sm.ResumeAction("nope")
	assert.EqualError(err, "Action with ID 'nope' not found")
	err = sm.DeleteAction("testaction")
	assert.EqualError(err, "pop")

	_, err = sm.AddSubscription(nil, &kldbind.ABIEvent{Name: "any"}, "nope")
	assert.EqualError(err, "Action with ID 'nope' not found")
	_, err = sm.AddSubscription(nil, &kldbind.ABIEvent{Name: "any"}, "testaction")
	assert.EqualError(err, "Failed to store action: pop")
	sm.subscriptions["testsub"] = &subscription{info: &SubscriptionInfo{}, rpc: sm.rpc}
	err = sm.DeleteSubscription("nope")
	assert.EqualError(err, "Subscription with ID 'nope' not found")
	err = sm.DeleteSubscription("testsub")
	assert.EqualError(err, "pop")
}
