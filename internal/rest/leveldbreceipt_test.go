// Copyright 2021 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rest

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path"
	"testing"
	"time"

	"github.com/kaleido-io/ethconnect/internal/kvstore"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
)

var tmpdir string

type mockKVStore struct {
	err error
}

func (m *mockKVStore) Get(key string) ([]byte, error) {
	return nil, m.err
}
func (m *mockKVStore) Put(key string, val []byte) error {
	return m.err
}
func (m *mockKVStore) Delete(key string) error {
	return m.err
}
func (m *mockKVStore) NewIterator(keyRange ...interface{}) kvstore.KVIterator {
	return nil
}

func (m *mockKVStore) Close() {
	return
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	teardown()
	os.Exit(code)
}

func setup() {
	tmpdir, _ = ioutil.TempDir("", "leveldbreceipt_test")
	// create a file to use as the path for LevelDB in order to generate an error
	ioutil.WriteFile(path.Join(tmpdir, "dummyfile"), []byte("dummy content"), 0644)
}

func teardown() {
	os.RemoveAll(tmpdir)
}

func TestNewLevelDBReceiptsCreateOK(t *testing.T) {
	assert := assert.New(t)
	conf := &LevelDBReceiptStoreConf{
		Path: tmpdir,
	}
	r, err := newLevelDBReceipts(conf)
	defer r.store.Close()
	assert.Equal(conf, r.conf)
	assert.Nil(err)
	assert.NotNil(r.store)
}

func TestLevelDBReceiptCreateErr(t *testing.T) {
	assert := assert.New(t)
	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "dummyfile"),
	}
	_, err := newLevelDBReceipts(conf)
	assert.Regexp("Unable to open LevelDB: .*", err)
}

func TestLevelDBReceiptsAddReceiptOK(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: tmpdir,
	}
	r, err := newLevelDBReceipts(conf)
	defer r.store.Close()

	receipt := make(map[string]interface{})
	receipt["prop1"] = "value1"
	err = r.AddReceipt("r0", &receipt)
	assert.NoError(err)

	itr := r.store.NewIterator()
	i := 0
	for itr.Next() {
		if i == 0 {
			val := itr.Value()
			assert.Equal("z", string(val[0]))
			doc, err := r.store.Get(string(val))
			assert.NoError(err)
			decoded := make(map[string]interface{})
			json.Unmarshal(doc, &decoded)
			assert.Equal("value1", decoded["prop1"])
		}
		i++
	}
}

func TestLevelDBReceiptsAddReceiptFailed(t *testing.T) {
	assert := assert.New(t)

	kvstoreMock := &mockKVStore{
		err: fmt.Errorf("pop"),
	}
	t1 := time.Unix(1000000, 0)
	entropy := ulid.Monotonic(rand.New(rand.NewSource(t1.UnixNano())), 0)
	r := &levelDBReceipts{
		conf:      &LevelDBReceiptStoreConf{},
		store:     kvstoreMock,
		idEntropy: entropy,
	}

	receipt := make(map[string]interface{})
	err := r.AddReceipt("key", &receipt)
	assert.EqualError(err, "pop")
}

func TestLevelDBReceiptsGetReceiptsOK(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test1"),
	}
	r, err := newLevelDBReceipts(conf)
	defer r.store.Close()

	id1 := "a492bc53-d971-4a9b-7d8c-d420a10d0aad"
	receipt1 := make(map[string]interface{})
	receipt1["_id"] = id1
	receipt1["prop1"] = "value2"
	err = r.AddReceipt(id1, &receipt1)

	id2 := "f1ac18f4-97ad-42e6-673d-64a9f6376993"
	receipt2 := make(map[string]interface{})
	receipt2["_id"] = id1
	receipt2["prop1"] = "value1"
	err = r.AddReceipt(id2, &receipt2)

	id3 := "186eb2db-a098-4eaf-718c-efa047870830"
	receipt3 := make(map[string]interface{})
	receipt3["_id"] = id3
	receipt3["prop1"] = "value3"
	err = r.AddReceipt(id3, &receipt3)

	results, err := r.GetReceipts(0, 0, nil, 0, "", "", "")
	assert.NoError(err)
	assert.Equal(3, len(*results))
	assert.Equal("value3", (*results)[0]["prop1"])
	assert.Equal("value1", (*results)[1]["prop1"])
	assert.Equal("value2", (*results)[2]["prop1"])
}

func TestLevelDBReceiptsGetReceiptsWithStart(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test1"),
	}
	r, err := newLevelDBReceipts(conf)
	defer r.store.Close()

	id1 := "a492bc53-d971-4a9b-7d8c-d420a10d0aad"
	receipt1 := make(map[string]interface{})
	receipt1["_id"] = id1
	receipt1["prop1"] = "value2"
	receipt1["from"] = "0xc1f617aa2e1b22be21b5ef4a93d49678533a9662"
	err = r.AddReceipt(id1, &receipt1)

	id2 := "f1ac18f4-97ad-42e6-673d-64a9f6376993"
	receipt2 := make(map[string]interface{})
	receipt2["_id"] = id1
	receipt2["prop1"] = "value1"
	receipt2["from"] = "0xc1f617aa2e1b22be21b5ef4a93d49678533a9662"
	err = r.AddReceipt(id2, &receipt2)

	id3 := "186eb2db-a098-4eaf-718c-efa047870830"
	receipt3 := make(map[string]interface{})
	receipt3["_id"] = id3
	receipt3["prop1"] = "value3"
	receipt3["from"] = "0xc1f617aa2e1b22be21b5ef4a93d49678533a9662"
	err = r.AddReceipt(id3, &receipt3)

	itr := r.store.NewIterator()
	i := 0
	var startKey string
	valid := itr.Last()
	for ; valid; valid = itr.Prev() {
		if i == 1 {
			startKey = itr.Key()
			break
		}
		i++
	}

	results, err := r.GetReceipts(0, 2, nil, 0, "", "", startKey)
	assert.NoError(err)
	assert.Equal(2, len(*results))
	assert.Equal("value1", (*results)[0]["prop1"])
	assert.Equal(startKey, (*results)[0]["_sequenceKey"])
	assert.Equal("value2", (*results)[1]["prop1"])
}

func TestLevelDBReceiptsFilterByIDs(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test2"),
	}
	r, err := newLevelDBReceipts(conf)
	defer r.store.Close()

	now := time.Now()
	var receivedAt int64
	receivedAt = int64(now.UnixNano() / int64(time.Millisecond))

	receipt1 := make(map[string]interface{})
	receipt1["_id"] = "r1"
	receipt1["prop1"] = "value1"
	receipt1["receivedAt"] = receivedAt
	receipt1["from"] = "addr1"
	receipt1["to"] = "addr2"
	err = r.AddReceipt("r1", &receipt1)

	receipt2 := make(map[string]interface{})
	receipt2["_id"] = "r2"
	receipt2["prop1"] = "value2"
	receipt2["receivedAt"] = receivedAt
	receipt2["from"] = "addr1"
	receipt2["to"] = "addr2"
	err = r.AddReceipt("r2", &receipt2)

	receipt3 := make(map[string]interface{})
	receipt3["_id"] = "r3"
	receipt3["prop1"] = "value3"
	receipt3["receivedAt"] = receivedAt
	receipt3["from"] = "addr1"
	err = r.AddReceipt("r3", &receipt3)

	results, err := r.GetReceipts(1, 2, []string{"r1", "r2"}, int64((now.UnixNano()/int64(time.Millisecond))-10), "addr1", "addr2", "")
	assert.NoError(err)
	assert.Equal(2, len(*results))
	assert.Equal("value1", (*results)[0]["prop1"])
	assert.Equal("value2", (*results)[1]["prop1"])
}

func TestLevelDBReceiptsFilterByIDsAndFromTo(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test2"),
	}
	r, err := newLevelDBReceipts(conf)
	defer r.store.Close()

	now := time.Now()
	var receivedAt int64
	receivedAt = int64(now.UnixNano() / int64(time.Millisecond))

	receipt1 := make(map[string]interface{})
	receipt1["_id"] = "r1"
	receipt1["prop1"] = "value1"
	receipt1["receivedAt"] = receivedAt
	receipt1["from"] = "addr1"
	receipt1["to"] = "addr2"
	err = r.AddReceipt("r1", &receipt1)

	receipt2 := make(map[string]interface{})
	receipt2["_id"] = "r2"
	receipt2["prop1"] = "value2"
	receipt2["receivedAt"] = receivedAt
	receipt2["from"] = "addr1.1"
	receipt2["to"] = "addr2"
	err = r.AddReceipt("r2", &receipt2)

	receipt3 := make(map[string]interface{})
	receipt3["_id"] = "r3"
	receipt3["prop1"] = "value3"
	receipt3["receivedAt"] = receivedAt
	receipt3["from"] = "addr1"
	err = r.AddReceipt("r3", &receipt3)

	results, err := r.GetReceipts(1, 2, []string{"r1", "r2"}, 0, "addr1", "addr2", "")
	assert.NoError(err)
	assert.Equal(1, len(*results))
	assert.Equal("value1", (*results)[0]["prop1"])
}

func TestLevelDBReceiptsFilterNotFound(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test3"),
	}
	r, err := newLevelDBReceipts(conf)
	defer r.store.Close()

	now := time.Now()
	var receivedAt int64
	receivedAt = int64(now.UnixNano() / int64(time.Millisecond))

	receipt1 := make(map[string]interface{})
	receipt1["_id"] = "r1"
	receipt1["prop1"] = "value1"
	receipt1["receivedAt"] = receivedAt
	receipt1["from"] = "addr1"
	receipt1["to"] = "addr2"
	err = r.AddReceipt("r1", &receipt1)

	receipt2 := make(map[string]interface{})
	receipt2["_id"] = "r2"
	receipt2["prop1"] = "value2"
	receipt2["receivedAt"] = receivedAt
	receipt2["from"] = "addr1"
	receipt2["to"] = "addr2"
	err = r.AddReceipt("r2", &receipt2)

	receipt3 := make(map[string]interface{})
	receipt3["_id"] = "r3"
	receipt3["prop1"] = "value3"
	receipt3["receivedAt"] = receivedAt
	err = r.AddReceipt("r3", &receipt3)

	// not found due to IDs
	results, err := r.GetReceipts(0, 2, []string{"r4", "r5"}, int64((now.UnixNano()/int64(time.Millisecond))-10), "addr1", "addr2", "")
	assert.NoError(err)
	assert.Len(*results, 0)

	// not found due to epoch
	results, err = r.GetReceipts(0, 2, []string{"r1", "r2"}, int64((now.UnixNano()/int64(time.Millisecond))+10), "addr1", "addr2", "")
	assert.NoError(err)
	assert.Len(*results, 0)

	// not found due to From address
	results, err = r.GetReceipts(0, 2, []string{"r1", "r2"}, int64((now.UnixNano()/int64(time.Millisecond))-10), "addr4", "addr2", "")
	assert.NoError(err)
	assert.Len(*results, 0)

	// not found due to To address
	results, err = r.GetReceipts(0, 2, []string{"r1", "r2"}, int64((now.UnixNano()/int64(time.Millisecond))-10), "addr1", "addr4", "")
	assert.NoError(err)
	assert.Len(*results, 0)
}

func TestLevelDBReceiptsGetReceiptOK(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test4"),
	}
	r, err := newLevelDBReceipts(conf)
	defer r.store.Close()
	receipt1 := make(map[string]interface{})
	receipt1["_id"] = "r1"
	receipt1["prop1"] = "value1"
	receipt1["from"] = "addr1"
	receipt1["to"] = "addr2"
	err = r.AddReceipt("r1", &receipt1)

	result, err := r.GetReceipt("r1")
	assert.NoError(err)
	assert.Equal("r1", (*result)["_id"])
	assert.Equal("value1", (*result)["prop1"])
}

func TestLevelDBReceiptsGetReceiptNotFound(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test4"),
	}
	r, _ := newLevelDBReceipts(conf)
	defer r.store.Close()

	result, err := r.GetReceipt("receipt1")
	assert.NoError(err)
	assert.Nil(result)
}

func TestLevelDBReceiptsGetReceiptError(t *testing.T) {
	assert := assert.New(t)

	kvstoreMock := &mockKVStore{
		err: fmt.Errorf("pop"),
	}
	r := &levelDBReceipts{
		conf:  &LevelDBReceiptStoreConf{},
		store: kvstoreMock,
	}

	_, err := r.GetReceipt("receipt1")
	assert.EqualError(err, "Failed to the entry for the original key: receipt1. pop")
}
