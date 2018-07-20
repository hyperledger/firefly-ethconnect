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
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/globalsign/mgo"
	"github.com/stretchr/testify/assert"

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
	inserted  map[string]interface{}
	insertErr error
	collInfo  *mgo.CollectionInfo
	collErr   error
}

func (m *mockCollection) Insert(payloads ...interface{}) error {
	m.inserted = payloads[0].(map[string]interface{})
	return m.insertErr
}

func (m *mockCollection) Create(info *mgo.CollectionInfo) error {
	m.collInfo = info
	return m.collErr
}

func TestReplyProcessorWithValidReply(t *testing.T) {
	assert := assert.New(t)

	w := NewWebhooksBridge()
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

	w := NewWebhooksBridge()
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

	w := NewWebhooksBridge()
	mockCollection := &mockCollection{}
	w.mongo = mockCollection

	emptyMsg := make(map[string]interface{})
	msgBytes, _ := json.Marshal(&emptyMsg)
	w.processReply(msgBytes)

	assert.Nil(mockCollection.inserted)

}

func TestReplyProcessorMissingRequestId(t *testing.T) {
	assert := assert.New(t)

	w := NewWebhooksBridge()
	mockCollection := &mockCollection{}
	w.mongo = mockCollection

	replyMsg := &kldmessages.ErrorReply{}
	replyMsgBytes, _ := json.Marshal(&replyMsg)

	w.processReply(replyMsgBytes)

	assert.Nil(mockCollection.inserted)

}

func TestReplyProcessorInsertError(t *testing.T) {
	assert := assert.New(t)

	w := NewWebhooksBridge()
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
	w := NewWebhooksBridge()
	mockMongo := &mockMongo{connErr: fmt.Errorf("bang")}
	w.conf.MongoDB.URL = "mongodb://localhost:27017"
	err := w.connectMongoDB(mockMongo)
	assert.Regexp("Unable to connect to MongoDB: bang", err.Error())
}

func TestConnectMongoDBConnectCreateCollection(t *testing.T) {
	assert := assert.New(t)
	w := NewWebhooksBridge()
	mockMongo := &mockMongo{}
	w.conf.MongoDB.URL = "mongodb://localhost:27017"
	err := w.connectMongoDB(mockMongo)
	assert.Nil(err)
	assert.False(mockMongo.collection.collInfo.Capped)
}

func TestConnectMongoDBConnectCreateCappedCollection(t *testing.T) {
	assert := assert.New(t)
	w := NewWebhooksBridge()
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
	w := NewWebhooksBridge()
	mockMongo := &mockMongo{}
	mockMongo.collection.collErr = fmt.Errorf("snap")
	w.conf.MongoDB.URL = "mongodb://localhost:27017"
	err := w.connectMongoDB(mockMongo)
	assert.Nil(err)
}
