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

package kldrest

import (
	"fmt"
	"testing"
	"time"

	"github.com/globalsign/mgo"
	"github.com/globalsign/mgo/bson"
	"github.com/stretchr/testify/assert"
)

type mockMongo struct {
	connErr        error
	collection     mockCollection
	url            string
	databaseName   string
	collectionName string
}

func (m *mockMongo) Connect(url string, timeout time.Duration) (err error) {
	m.url = url
	return m.connErr
}

func (m *mockMongo) GetCollection(database string, collection string) MongoCollection {
	m.databaseName = database
	m.collectionName = collection
	return &m.collection
}

type mockCollection struct {
	inserted       map[string]interface{}
	insertErr      error
	collInfo       *mgo.CollectionInfo
	collErr        error
	ensureIndexErr error
	mockQuery      mockQuery
	captureQuery   interface{}
}

func (m *mockCollection) Insert(payloads ...interface{}) error {
	m.inserted = payloads[0].(map[string]interface{})
	return m.insertErr
}

func (m *mockCollection) Create(info *mgo.CollectionInfo) error {
	m.collInfo = info
	return m.collErr
}

func (m *mockCollection) Find(query interface{}) MongoQuery {
	m.captureQuery = query
	return &m.mockQuery
}

func (m *mockCollection) EnsureIndex(index mgo.Index) error {
	return m.ensureIndexErr
}

type mockQuery struct {
	allErr        error
	oneErr        error
	resultWranger func(interface{})
	limit         int
	skip          int
	sort          []string
}

func (m *mockQuery) Limit(n int) *mgo.Query {
	m.limit = n
	return nil
}

func (m *mockQuery) Skip(n int) *mgo.Query {
	m.skip = n
	return nil
}

func (m *mockQuery) Sort(fields ...string) *mgo.Query {
	m.sort = fields
	return nil
}

func (m *mockQuery) All(result interface{}) error {
	if m.resultWranger != nil {
		m.resultWranger(result)
	}
	return m.allErr
}

func (m *mockQuery) One(result interface{}) error {
	if m.resultWranger != nil {
		m.resultWranger(result)
	}
	return m.oneErr
}

func TestNewMongoReceipts(t *testing.T) {
	assert := assert.New(t)
	conf := &MongoDBReceiptStoreConf{}
	r := newMongoReceipts(conf)
	assert.Equal(conf, r.conf)
	return
}

func TestMongoReceiptsConnectOK(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{connErr: nil}
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{
			ReceiptStoreConf: ReceiptStoreConf{MaxDocs: 123},
			URL:              "testurl",
			Database:         "testdb",
			Collection:       "testcoll",
		},
		mgo: mgoMock,
	}

	err := r.connect()
	assert.NoError(err)
	assert.Equal("testurl", mgoMock.url)
	assert.Equal("testdb", mgoMock.databaseName)
	assert.Equal("testcoll", mgoMock.collectionName)
	assert.Equal(true, mgoMock.collection.collInfo.Capped)
	assert.Equal(123, mgoMock.collection.collInfo.MaxDocs)

	return
}

func TestMongoReceiptsConnectConnErr(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{connErr: fmt.Errorf("pop")}
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	err := r.connect()
	assert.EqualError(err, "Unable to connect to MongoDB: pop")
	return
}

func TestMongoReceiptsConnectCollErr(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{}
	mgoMock.collection.collErr = fmt.Errorf("pop")
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	err := r.connect()
	assert.NoError(err)
	return
}

func TestMongoReceiptsConnectIdxErr(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{}
	mgoMock.collection.ensureIndexErr = fmt.Errorf("pop")
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	err := r.connect()
	assert.EqualError(err, "Unable to create index: pop")
	return
}

func TestMongoReceiptsAddReceiptOK(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{}
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	r.connect()
	receipt := make(map[string]interface{})
	err := r.AddReceipt(&receipt)
	assert.NoError(err)
	return
}

func TestMongoReceiptsAddReceiptFailed(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{}
	mgoMock.collection.insertErr = fmt.Errorf("pop")
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	r.connect()
	receipt := make(map[string]interface{})
	err := r.AddReceipt(&receipt)
	assert.EqualError(err, "pop")
	return
}

func TestMongoReceiptsGetReceiptsOK(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{}
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	mgoMock.collection.mockQuery.resultWranger = func(result interface{}) {
		resArray := result.(*[]map[string]interface{})
		res1 := make(map[string]interface{})
		res1["key1"] = "value1"
		*resArray = append(*resArray, res1)
		res2 := make(map[string]interface{})
		res2["key2"] = "value2"
		*resArray = append(*resArray, res2)
		return
	}

	r.connect()
	results, err := r.GetReceipts(5, 2, nil, 0, "", "")
	assert.NoError(err)
	assert.Equal(5, mgoMock.collection.mockQuery.skip)
	assert.Equal(2, mgoMock.collection.mockQuery.limit)
	assert.Equal("value1", (*results)[0]["key1"])
	assert.Equal("value2", (*results)[1]["key2"])
	return
}

func TestMongoReceiptsFilter(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{}
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	mgoMock.collection.mockQuery.resultWranger = func(result interface{}) {
		resArray := result.(*[]map[string]interface{})
		res1 := make(map[string]interface{})
		res1["key1"] = "value1"
		*resArray = append(*resArray, res1)
		res2 := make(map[string]interface{})
		res2["key2"] = "value2"
		*resArray = append(*resArray, res2)
		return
	}

	r.connect()
	now := time.Now()
	results, err := r.GetReceipts(0, 0, []string{"key1", "key2"}, now.UnixNano()/int64(time.Millisecond), "addr1", "addr2")
	assert.NoError(err)
	queryBSON := mgoMock.collection.captureQuery.(bson.M)
	assert.Equal([]string{"key1", "key2"}, queryBSON["_id"].(bson.M)["$in"])
	assert.Equal(now.UnixNano()/int64(time.Millisecond), queryBSON["receivedAt"].(bson.M)["$gt"])
	assert.Equal("addr1", queryBSON["from"])
	assert.Equal("addr2", queryBSON["to"])
	assert.Equal(0, mgoMock.collection.mockQuery.skip)
	assert.Equal(0, mgoMock.collection.mockQuery.limit)
	assert.Equal("value1", (*results)[0]["key1"])
	assert.Equal("value2", (*results)[1]["key2"])
	return
}

func TestMongoReceiptsGetReceiptsNotFound(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{}
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	mgoMock.collection.mockQuery.allErr = mgo.ErrNotFound

	r.connect()
	results, err := r.GetReceipts(5, 2, nil, 0, "", "")
	assert.NoError(err)
	assert.Len(*results, 0)
	return
}

func TestMongoReceiptsGetReceiptsError(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{}
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	mgoMock.collection.mockQuery.allErr = fmt.Errorf("pop")

	r.connect()
	_, err := r.GetReceipts(5, 2, nil, 0, "", "")
	assert.EqualError(err, "pop")
	return
}

func TestMongoReceiptsGetReceiptOK(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{}
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	mgoMock.collection.mockQuery.resultWranger = func(result interface{}) {
		resMap := result.(*map[string]interface{})
		res1 := make(map[string]interface{})
		res1["_id"] = "receipt1"
		res1["key1"] = "value1"
		*resMap = res1
		return
	}

	r.connect()
	result, err := r.GetReceipt("receipt1")
	assert.NoError(err)
	assert.Equal("receipt1", (*result)["_id"])
	assert.Equal("value1", (*result)["key1"])
	return
}

func TestMongoReceiptsGetReceiptNotFound(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{}
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	mgoMock.collection.mockQuery.oneErr = mgo.ErrNotFound

	r.connect()
	result, err := r.GetReceipt("receipt1")
	assert.NoError(err)
	assert.Nil(result)
	return
}

func TestMongoReceiptsGetReceiptError(t *testing.T) {
	assert := assert.New(t)

	mgoMock := &mockMongo{}
	r := &mongoReceipts{
		conf: &MongoDBReceiptStoreConf{},
		mgo:  mgoMock,
	}

	mgoMock.collection.mockQuery.oneErr = fmt.Errorf("pop")

	r.connect()
	_, err := r.GetReceipt("receipt1")
	assert.EqualError(err, "pop")
	return
}
