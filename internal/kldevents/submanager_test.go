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
	"io/ioutil"
	"net/http/httptest"
	"os"
	"path"
	"reflect"
	"testing"

	"github.com/julienschmidt/httprouter"
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

func newTestSubscriptionManager(dir string) *subscriptionManager {
	smconf := &SubscriptionManagerConf{
		LevelDBPath: path.Join(dir, "db"),
	}
	rconf := &kldeth.RPCConnOpts{URL: ""}
	sm := NewSubscriptionManager(smconf, rconf).(*subscriptionManager)
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
