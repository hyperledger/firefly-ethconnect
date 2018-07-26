// Copyright 2018 Kaleido, a ConsenSys business

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldwebhooks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/Shopify/sarama"
	"github.com/ethereum/go-ethereum/common"
	"github.com/globalsign/mgo"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/kaleido-io/ethconnect/internal/kldkafka"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
)

type mockMongo struct {
	connErr    error
	collection mockCollection
}

func (m *mockMongo) Connect(url string) (err error) {
	return m.connErr
}

func (m *mockMongo) GetCollection(database string, collection string) MongoCollection {
	return &m.collection
}

type mockCollection struct {
	inserted       map[string]interface{}
	insertErr      error
	collInfo       *mgo.CollectionInfo
	collErr        error
	ensureIndexErr error
	mockQuery      mockQuery
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

func TestReplyProcessorWithValidReply(t *testing.T) {
	assert := assert.New(t)

	var printYAML = false
	w := NewWebhooksBridge(&printYAML)
	mockCollection := &mockCollection{}
	w.mongo = mockCollection

	replyMsg := &kldmessages.TransactionReceipt{}
	replyMsg.Headers.MsgType = kldmessages.MsgTypeTransactionSuccess
	replyMsg.Headers.ID = kldutils.UUIDv4()
	replyMsg.Headers.ReqID = kldutils.UUIDv4()
	replyMsg.Headers.ReqOffset = "topic:1:2"
	txHash := common.HexToHash("0x02587104e9879911bea3d5bf6ccd7e1a6cb9a03145b8a1141804cebd6aa67c5c")
	replyMsg.TransactionHash = &txHash
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	w.processReply(replyMsgBytes)

	assert.NotNil(mockCollection.inserted)
	assert.Equal(replyMsg.Headers.ReqID, mockCollection.inserted["_id"])

}

func TestReplyProcessorWithErrorReply(t *testing.T) {
	assert := assert.New(t)

	var printYAML = false
	w := NewWebhooksBridge(&printYAML)
	mockCollection := &mockCollection{}
	w.mongo = mockCollection

	replyMsg := &kldmessages.ErrorReply{}
	replyMsg.Headers.MsgType = kldmessages.MsgTypeError
	replyMsg.Headers.ID = kldutils.UUIDv4()
	replyMsg.Headers.ReqID = kldutils.UUIDv4()
	replyMsg.Headers.ReqOffset = "topic:1:2"
	replyMsg.OriginalMessage = "{\"badness\": true}"
	replyMsg.ErrorMessage = "pop"
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	w.processReply(replyMsgBytes)

	assert.NotNil(mockCollection.inserted)
	assert.Equal(replyMsg.Headers.ReqID, mockCollection.inserted["_id"])
	assert.Equal(replyMsg.ErrorMessage, mockCollection.inserted["errorMessage"])
	assert.Equal(replyMsg.OriginalMessage, mockCollection.inserted["requestPayload"])

}

func TestReplyProcessorMissingHeaders(t *testing.T) {
	assert := assert.New(t)

	var printYAML = false
	w := NewWebhooksBridge(&printYAML)
	mockCollection := &mockCollection{}
	w.mongo = mockCollection

	emptyMsg := make(map[string]interface{})
	msgBytes, _ := json.Marshal(&emptyMsg)
	w.processReply(msgBytes)

	assert.Nil(mockCollection.inserted)

}

func TestReplyProcessorMissingRequestId(t *testing.T) {
	assert := assert.New(t)

	var printYAML = false
	w := NewWebhooksBridge(&printYAML)
	mockCollection := &mockCollection{}
	w.mongo = mockCollection

	replyMsg := &kldmessages.ErrorReply{}
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	w.processReply(replyMsgBytes)

	assert.Nil(mockCollection.inserted)

}

func TestReplyProcessorInsertError(t *testing.T) {
	assert := assert.New(t)

	var printYAML = false
	w := NewWebhooksBridge(&printYAML)
	mockCollection := &mockCollection{insertErr: fmt.Errorf("pop")}
	w.mongo = mockCollection

	replyMsg := &kldmessages.ErrorReply{}
	replyMsg.Headers.ReqID = kldutils.UUIDv4()
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	w.processReply(replyMsgBytes)

	assert.NotNil(mockCollection.inserted)

}

func TestConnectMongoDBConnectFailure(t *testing.T) {
	assert := assert.New(t)
	var printYAML = false
	w := NewWebhooksBridge(&printYAML)
	mockMongo := &mockMongo{connErr: fmt.Errorf("bang")}
	w.conf.MongoDB.URL = "mongodb://localhost:27017"
	err := w.connectMongoDB(mockMongo)
	assert.Regexp("Unable to connect to MongoDB: bang", err.Error())
}

func TestConnectMongoDBConnectCreateCollection(t *testing.T) {
	assert := assert.New(t)
	var printYAML = false
	w := NewWebhooksBridge(&printYAML)
	mockMongo := &mockMongo{}
	w.conf.MongoDB.URL = "mongodb://localhost:27017"
	err := w.connectMongoDB(mockMongo)
	assert.Nil(err)
	assert.False(mockMongo.collection.collInfo.Capped)
}

func TestConnectMongoDBConnectCreateCappedCollection(t *testing.T) {
	assert := assert.New(t)
	var printYAML = false
	w := NewWebhooksBridge(&printYAML)
	mockMongo := &mockMongo{}
	w.conf.MongoDB.URL = "mongodb://localhost:27017"
	w.conf.MongoDB.MaxDocs = 1000
	err := w.connectMongoDB(mockMongo)
	assert.Nil(err)
	assert.True(mockMongo.collection.collInfo.Capped)
	assert.Equal(1000, mockMongo.collection.collInfo.MaxDocs)
}

func TestConnectMongoDBConnectCollectionExists(t *testing.T) {
	assert := assert.New(t)
	var printYAML = false
	w := NewWebhooksBridge(&printYAML)
	mockMongo := &mockMongo{}
	mockMongo.collection.collErr = fmt.Errorf("snap")
	w.conf.MongoDB.URL = "mongodb://localhost:27017"
	err := w.connectMongoDB(mockMongo)
	assert.Nil(err)
}

func TestConnectMongoDBIndexCreationFailure(t *testing.T) {
	assert := assert.New(t)
	var printYAML = false
	w := NewWebhooksBridge(&printYAML)
	mockMongo := &mockMongo{}
	mockMongo.collection.ensureIndexErr = fmt.Errorf("crack")
	w.conf.MongoDB.URL = "mongodb://localhost:27017"
	err := w.connectMongoDB(mockMongo)
	assert.Regexp("Unable to create index: crack", err.Error())
}

func testReplyCall(assert *assert.Assertions, coll MongoCollection, url string) (resp *http.Response) {
	k := newTestKafkaComon()
	w, _ := startTestWebhooks(nil, k)
	w.mongo = coll
	resp, httpErr := http.Get(url)
	if httpErr != nil {
		log.Errorf("HTTP error for %s: %+v", url, httpErr)
	}
	assert.Nil(httpErr)
	k.stop <- true
	return resp
}

func TestGetReplyNoStore(t *testing.T) {
	assert := assert.New(t)
	var nilMongo MongoCollection
	resp := testReplyCall(assert, nilMongo, fmt.Sprintf("http://localhost:%d/reply/ABCDEFG", lastPort))
	assert.Equal(405, resp.StatusCode)
}

func TestGetReplyMissing(t *testing.T) {
	assert := assert.New(t)
	mockCol := &mockCollection{}
	mockCol.mockQuery.oneErr = mgo.ErrNotFound
	resp := testReplyCall(assert, mockCol, fmt.Sprintf("http://localhost:%d/reply/ABCDEFG", lastPort))
	assert.Equal(404, resp.StatusCode)
}

func TestGetReplyError(t *testing.T) {
	assert := assert.New(t)
	mockCol := &mockCollection{}
	mockCol.mockQuery.oneErr = fmt.Errorf("pop")
	resp := testReplyCall(assert, mockCol, fmt.Sprintf("http://localhost:%d/reply/ABCDEFG", lastPort))
	assert.Equal(500, resp.StatusCode)
}

func TestGetReplyOK(t *testing.T) {
	assert := assert.New(t)
	mockCol := &mockCollection{}
	resp := testReplyCall(assert, mockCol, fmt.Sprintf("http://localhost:%d/reply/ABCDEFG", lastPort))
	assert.Equal(200, resp.StatusCode)
}

func TestGetReplyUnSerializable(t *testing.T) {
	assert := assert.New(t)
	mockCol := &mockCollection{}
	mockCol.mockQuery.resultWranger = func(result interface{}) {
		unserializable := make(map[bool]interface{})
		unserializable[false] = "going to happen"
		(*result.(*map[string]interface{}))["key"] = unserializable
	}
	resp := testReplyCall(assert, mockCol, fmt.Sprintf("http://localhost:%d/reply/ABCDEFG", lastPort))
	assert.Equal(500, resp.StatusCode)
}

func TestGetRepliesNoStore(t *testing.T) {
	assert := assert.New(t)
	var nilMongo MongoCollection
	resp := testReplyCall(assert, nilMongo, fmt.Sprintf("http://localhost:%d/replies", lastPort))
	assert.Equal(405, resp.StatusCode)
}

func TestGetRepliesMissing(t *testing.T) {
	assert := assert.New(t)
	mockCol := &mockCollection{}
	mockCol.mockQuery.allErr = mgo.ErrNotFound
	resp := testReplyCall(assert, mockCol, fmt.Sprintf("http://localhost:%d/replies", lastPort))
	assert.Equal(404, resp.StatusCode)
}

func TestGetRepliesError(t *testing.T) {
	assert := assert.New(t)
	mockCol := &mockCollection{}
	mockCol.mockQuery.allErr = fmt.Errorf("pop")
	resp := testReplyCall(assert, mockCol, fmt.Sprintf("http://localhost:%d/replies", lastPort))
	assert.Equal(500, resp.StatusCode)
}

func TestGetRepliesDefaultLimit(t *testing.T) {
	assert := assert.New(t)
	mockCol := &mockCollection{}
	resp := testReplyCall(assert, mockCol, fmt.Sprintf("http://localhost:%d/replies", lastPort))
	assert.Equal(200, resp.StatusCode)
}

func TestGetRepliesCustomSkipLimit(t *testing.T) {
	assert := assert.New(t)
	mockCol := &mockCollection{}
	resp := testReplyCall(assert, mockCol, fmt.Sprintf("http://localhost:%d/replies?limit=50&skip=10", lastPort))
	assert.Equal(50, mockCol.mockQuery.limit)
	assert.Equal(10, mockCol.mockQuery.skip)
	assert.Equal(200, resp.StatusCode)
}

func TestGetRepliesInvalidSkipLimit(t *testing.T) {
	assert := assert.New(t)
	mockCol := &mockCollection{}
	resp := testReplyCall(assert, mockCol, fmt.Sprintf("http://localhost:%d/replies?limit=bad&skip=ness", lastPort))
	assert.Equal(10, mockCol.mockQuery.limit)
	assert.Equal(0, mockCol.mockQuery.skip)
	assert.Equal(200, resp.StatusCode)
}

func TestGetRepliesExcessiveLimit(t *testing.T) {
	assert := assert.New(t)
	mockCol := &mockCollection{}
	resp := testReplyCall(assert, mockCol, fmt.Sprintf("http://localhost:%d/replies?limit=1000", lastPort))
	assert.Equal(100, mockCol.mockQuery.limit)
	assert.Equal(0, mockCol.mockQuery.skip)
	assert.Equal(200, resp.StatusCode)
}

func TestConsumerMessagesLoopCallsReplyProcessorWithEmptyPayload(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	w, err := startTestWebhooks(nil, k)
	assert.Nil(err)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	consumer, _ := k.kafkaFactory.NewConsumer(k)
	producer, _ := k.kafkaFactory.NewProducer(k)

	go func() {
		w.ConsumerMessagesLoop(consumer, producer, wg)
	}()

	consumer.(*kldkafka.MockKafkaConsumer).MockMessages <- &sarama.ConsumerMessage{
		Partition: 3,
		Offset:    12345,
		Value:     []byte(""),
	}

	k.stop <- true
	wg.Wait()

	assert.Equal(int64(12345), consumer.(*kldkafka.MockKafkaConsumer).OffsetsByPartition[3])

}
