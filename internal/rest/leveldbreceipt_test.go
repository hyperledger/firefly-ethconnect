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

	"github.com/hyperledger/firefly-ethconnect/internal/kvstore"
	"github.com/hyperledger/firefly-ethconnect/internal/utils"
	"github.com/oklog/ulid/v2"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var tmpdir string

type mockKVStore struct {
	getVal     []byte
	err        error
	getFailIdx int
	getCount   int
}

func (m *mockKVStore) Get(key string) ([]byte, error) {
	if m.getFailIdx > 0 && m.getFailIdx != m.getCount {
		m.getCount++
		return m.getVal, nil
	}
	return m.getVal, m.err
}
func (m *mockKVStore) Put(key string, val []byte) error {
	return m.err
}
func (m *mockKVStore) Delete(key string) error {
	return m.err
}
func (m *mockKVStore) NewIterator() kvstore.KVIterator {
	return nil
}
func (m *mockKVStore) NewIteratorWithRange(keyRange interface{}) kvstore.KVIterator {
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
	err = r.AddReceipt(utils.UUIDv4(), &receipt)
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

func TestLevelDBReceiptsAddReceiptIdempotencyCehck(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: tmpdir,
	}
	r, err := newLevelDBReceipts(conf)
	defer r.store.Close()

	receipt := make(map[string]interface{})
	receipt["prop1"] = "value1"
	uuid := utils.UUIDv4()
	err = r.AddReceipt(uuid, &receipt)
	assert.NoError(err)

	err = r.AddReceipt(uuid, &receipt)
	assert.Regexp("FFEC100219", err)
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
	assert.Regexp("pop", err)
}

func TestLevelDBReceiptsGetReceiptsOK(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test1"),
	}
	r, err := newLevelDBReceipts(conf)
	defer r.store.Close()

	id1 := utils.UUIDv4()
	receipt1 := make(map[string]interface{})
	receipt1["_id"] = id1
	receipt1["prop1"] = "value2"
	err = r.AddReceipt(id1, &receipt1)

	id2 := utils.UUIDv4()
	receipt2 := make(map[string]interface{})
	receipt2["_id"] = id1
	receipt2["prop1"] = "value1"
	err = r.AddReceipt(id2, &receipt2)

	id3 := utils.UUIDv4()
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

func TestLevelDBReceiptsGetReceiptsWithStartEnd(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test1"),
	}
	r, _ := newLevelDBReceipts(conf)
	defer r.store.Close()

	id1 := utils.UUIDv4()
	receipt1 := make(map[string]interface{})
	receipt1["_id"] = id1
	receipt1["prop1"] = "value1"
	receipt1["from"] = "0xc1f617aa2e1b22be21b5ef4a93d49678533a9662"
	receipt1["receivedAt"] = 1626405000000
	err := r.AddReceipt(id1, &receipt1)
	assert.NoError(err)

	id2 := utils.UUIDv4()
	receipt2 := make(map[string]interface{})
	receipt2["_id"] = id2
	receipt2["prop1"] = "value2"
	receipt2["from"] = "0xc1f617aa2e1b22be21b5ef4a93d49678533a9662"
	receipt1["receivedAt"] = 1626406000001
	err = r.AddReceipt(id2, &receipt2)
	assert.NoError(err)

	id3 := utils.UUIDv4()
	receipt3 := make(map[string]interface{})
	receipt3["_id"] = id3
	receipt3["prop1"] = "value3"
	receipt3["from"] = "0xc1f617aa2e1b22be21b5ef4a93d49678533a9662"
	receipt1["receivedAt"] = 1626407000002
	err = r.AddReceipt(id3, &receipt3)
	assert.NoError(err)

	id4 := utils.UUIDv4()
	receipt4 := make(map[string]interface{})
	receipt4["_id"] = id4
	receipt4["prop1"] = "value4"
	receipt4["from"] = "0xc1f617aa2e1b22be21b5ef4a93d49678533a9662"
	receipt1["receivedAt"] = 1626407000003
	err = r.AddReceipt(id4, &receipt4)
	assert.NoError(err)

	// Some test debug info
	itr := r.store.NewIterator()
	valid := itr.Next()
	for ; valid; valid = itr.Next() {
		b, _ := r.store.Get(itr.Key())
		log.Infof("%s: %s", itr.Key(), b)
	}
	endKey := r.findEndPoint(1626404000001)
	log.Infof("End key: %s", endKey)
	itr.Release()

	itr = r.store.NewIterator()
	i := 0
	var startKey string
	valid = itr.Last()
	for ; valid; valid = itr.Prev() {
		_, _ = r.store.Get(itr.Key())
		if i == 2 {
			startKey = itr.Key()
			break
		}
		i++
	}

	// start key is item at index 2, `since` is item at index 1, expecting result to be items at indexes 1 and 2
	results, err := r.GetReceipts(0, 2, nil, 1626404000001, "", "", startKey)
	assert.NoError(err)
	assert.Equal(2, len(*results))
	assert.Equal("value2", (*results)[0]["prop1"])
	assert.Equal(startKey, (*results)[0]["_sequenceKey"])
	assert.Equal("value1", (*results)[1]["prop1"])
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

	results, err := r.GetReceipts(1, 2, []string{"r1", "r2"}, int64((now.UnixNano()/int64(time.Millisecond))-10), "", "", "")
	assert.NoError(err)
	assert.Equal(2, len(*results))
	assert.Equal("value2", (*results)[0]["prop1"])
	assert.Equal("value1", (*results)[1]["prop1"])
}

func TestLevelDBReceiptsFilterByIDsAndFromTo(t *testing.T) {
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
	receipt2["from"] = "addr1.1"
	receipt2["to"] = "addr2"
	err = r.AddReceipt("r2", &receipt2)

	receipt3 := make(map[string]interface{})
	receipt3["_id"] = "r3"
	receipt3["prop1"] = "value3"
	receipt3["receivedAt"] = receivedAt
	receipt3["from"] = "addr1"
	err = r.AddReceipt("r3", &receipt3)

	results, err := r.GetReceipts(1, 3, []string{"r1", "r2"}, 0, "addr1", "addr2", "")
	assert.NoError(err)
	assert.Equal(1, len(*results))
	assert.Equal("value1", (*results)[0]["prop1"])
}

func TestLevelDBReceiptsFilterFromTo(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test4"),
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

	results, err := r.GetReceipts(1, 3, []string{}, 0, "addr1", "addr2", "")
	assert.NoError(err)
	assert.Equal(1, len(*results))
	assert.Equal("value1", (*results)[0]["prop1"])

	results, err = r.GetReceipts(1, 3, []string{}, 0, "addr1", "", "")
	assert.NoError(err)
	assert.Equal(2, len(*results))
	assert.Equal("value3", (*results)[0]["prop1"])
	assert.Equal("value1", (*results)[1]["prop1"])

	results, err = r.GetReceipts(1, 3, []string{}, 0, "", "addr2", "")
	assert.NoError(err)
	assert.Equal(2, len(*results))
	assert.Equal("value2", (*results)[0]["prop1"])
	assert.Equal("value1", (*results)[1]["prop1"])
}

func TestLevelDBReceiptsFilterNotFound(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test6"),
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
		Path: path.Join(tmpdir, "test7"),
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

func TestLevelDBReceiptsGetReceiptsUnmarshalFailIgnoreReceipt(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test8"),
	}
	r, err := newLevelDBReceipts(conf)
	defer r.store.Close()
	receipt1 := make(map[string]interface{})
	receipt1["_id"] = "r1"
	receipt1["prop1"] = "value1"
	receipt1["from"] = "addr1"
	receipt1["to"] = "addr2"
	err = r.store.Put("zr1", []byte("!json"))
	assert.NoError(err)

	results, err := r.GetReceipts(0, 1, nil, 0, "", "", "")
	assert.NoError(err)
	assert.Empty(results)
}

func TestLevelDBReceiptsGetReceiptNotFound(t *testing.T) {
	assert := assert.New(t)

	conf := &LevelDBReceiptStoreConf{
		Path: path.Join(tmpdir, "test6"),
	}
	r, _ := newLevelDBReceipts(conf)
	defer r.store.Close()

	result, err := r.GetReceipt("receipt1")
	assert.NoError(err)
	assert.Nil(result)
}

func TestLevelDBReceiptsGetReceiptErrorID(t *testing.T) {
	assert := assert.New(t)

	kvstoreMock := &mockKVStore{
		err: fmt.Errorf("pop"),
	}
	r := &levelDBReceipts{
		conf:  &LevelDBReceiptStoreConf{},
		store: kvstoreMock,
	}

	_, err := r.GetReceipt("receipt1")
	assert.Regexp("Failed to retrieve the entry for the original key: receipt1. pop", err)
}

func TestLevelDBReceiptsGetReceiptErrorGeneratedID(t *testing.T) {
	assert := assert.New(t)

	kvstoreMock := &mockKVStore{
		err:        fmt.Errorf("pop"),
		getFailIdx: 1,
		getVal:     []byte("generated-id"),
	}
	r := &levelDBReceipts{
		conf:  &LevelDBReceiptStoreConf{},
		store: kvstoreMock,
	}

	_, err := r.GetReceipt("receipt1")
	assert.Regexp("Failed to retrieve the entry for the generated ID: receipt1. pop", err)
}

func TestLevelDBReceiptsGetReceiptBadDataID(t *testing.T) {
	assert := assert.New(t)

	kvstoreMock := &mockKVStore{
		getVal: []byte("!json"),
	}
	r := &levelDBReceipts{
		conf:  &LevelDBReceiptStoreConf{},
		store: kvstoreMock,
	}

	_, err := r.GetReceipt("receipt1")
	assert.Regexp("invalid character", err)
}

func TestGetReceiptsByLookupKeyLimit(t *testing.T) {
	assert := assert.New(t)

	kvstoreMock := &mockKVStore{
		getVal: []byte("{}"),
	}
	r := &levelDBReceipts{
		conf:  &LevelDBReceiptStoreConf{},
		store: kvstoreMock,
	}

	results := r.getReceiptsByLookupKey([]string{"key1", "key2"}, 1)
	assert.Len(*results, 1)
}

func TestGetReceiptsByLookupKeyGetFail(t *testing.T) {
	assert := assert.New(t)

	kvstoreMock := &mockKVStore{
		err: fmt.Errorf("pop"),
	}
	r := &levelDBReceipts{
		conf:  &LevelDBReceiptStoreConf{},
		store: kvstoreMock,
	}

	results := r.getReceiptsByLookupKey([]string{"key1", "key2"}, 1)
	assert.Empty(results)
}

func TestGetReceiptsByLookupUnmarshalFail(t *testing.T) {
	assert := assert.New(t)

	kvstoreMock := &mockKVStore{
		getVal: []byte("!json"),
	}
	r := &levelDBReceipts{
		conf:  &LevelDBReceiptStoreConf{},
		store: kvstoreMock,
	}

	results := r.getReceiptsByLookupKey([]string{"key1", "key2"}, 1)
	assert.Empty(results)
}
