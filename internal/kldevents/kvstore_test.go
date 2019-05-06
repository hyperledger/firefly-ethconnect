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
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockKV struct {
	kvs map[string][]byte
	err error
}

func (m *mockKV) Put(key string, val []byte) error {
	m.kvs[key] = val
	return m.err
}
func (m *mockKV) Get(key string) ([]byte, error) {
	return m.kvs[key], m.err
}
func (m *mockKV) Delete(key string) error {
	delete(m.kvs, key)
	return m.err
}
func (m *mockKV) Close() {}

func newMockKV(err error) *mockKV {
	return &mockKV{
		err: err,
		kvs: make(map[string][]byte),
	}
}

func TestLevelDBPutGet(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)
	kv, err := newLDBKeyValueStore(path.Join(dir, "db"))
	assert.NoError(err)
	err = kv.Put("things", []byte("stuff"))
	assert.NoError(err)
	things, err := kv.Get("things")
	assert.NoError(err)
	assert.Equal("stuff", string(things))
	err = kv.Delete("things")
	assert.NoError(err)
	kv.Close()
}
