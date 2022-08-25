// Copyright 2018, 2021 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package receipts

import (
	"time"

	"github.com/globalsign/mgo"
	"github.com/globalsign/mgo/bson"
	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	log "github.com/sirupsen/logrus"
)

const (
	mongoConnectTimeout = 10 * 1000
)

type MongoReceipts struct {
	conf       *MongoDBReceiptStoreConf
	mgo        MongoDatabase
	collection MongoCollection
}

func NewMongoReceipts(conf *MongoDBReceiptStoreConf) *MongoReceipts {
	return &MongoReceipts{
		conf: conf,
		mgo:  &mgoWrapper{},
	}
}

func (m *MongoReceipts) Connect() (err error) {
	if m.conf.ConnectTimeoutMS <= 0 {
		m.conf.ConnectTimeoutMS = mongoConnectTimeout
	}
	err = m.mgo.Connect(m.conf.URL, time.Duration(m.conf.ConnectTimeoutMS)*time.Millisecond)
	if err != nil {
		err = errors.Errorf(errors.ReceiptStoreMongoDBConnect, err)
		return
	}
	m.collection = m.mgo.GetCollection(m.conf.Database, m.conf.Collection)
	if collErr := m.collection.Create(&mgo.CollectionInfo{
		Capped:  (m.conf.MaxDocs > 0),
		MaxDocs: m.conf.MaxDocs,
	}); collErr != nil {
		log.Infof("MongoDB collection exists: %s", err)
	}

	index := mgo.Index{
		Key:        []string{"receivedAt"},
		Unique:     false,
		DropDups:   false,
		Background: true,
		Sparse:     true,
	}
	if err = m.collection.EnsureIndex(index); err != nil {
		err = errors.Errorf(errors.ReceiptStoreMongoDBIndex, err)
		return
	}

	log.Infof("Connected to MongoDB on %s DB=%s Collection=%s", m.conf.URL, m.conf.Database, m.conf.Collection)
	return
}

// AddReceipt processes an individual reply message, and contains all errors
// To account for any transitory failures writing to mongoDB, it retries adding receipt with a backoff
func (m *MongoReceipts) AddReceipt(requestID string, receipt *map[string]interface{}, overwrite bool) (err error) {
	if overwrite {
		return m.collection.Upsert(bson.M{"_id": requestID}, *receipt)
	} else {
		return m.collection.Insert(*receipt)
	}
}

// GetReceipts Returns recent receipts with skip & limit
func (m *MongoReceipts) GetReceipts(skip, limit int, ids []string, sinceEpochMS int64, from, to, start string) (*[]map[string]interface{}, error) {
	filter := bson.M{}
	if len(ids) > 0 {
		filter["_id"] = bson.M{
			"$in": ids,
		}
	}
	if sinceEpochMS > 0 {
		filter["receivedAt"] = bson.M{
			"$gt": sinceEpochMS,
		}
	}
	if from != "" {
		filter["from"] = from
	}
	if to != "" {
		filter["to"] = to
	}
	query := m.collection.Find(filter)
	query.Sort("-receivedAt")
	if limit > 0 {
		query.Limit(limit)
	}
	if skip > 0 {
		query.Skip(skip)
	}
	// Perform the query
	var err error
	results := make([]map[string]interface{}, 0, limit)
	if err = query.All(&results); err != nil && err != mgo.ErrNotFound {
		return nil, err
	}
	return &results, nil
}

// getReply handles a HTTP request for an individual reply
func (m *MongoReceipts) GetReceipt(requestID string) (*map[string]interface{}, error) {
	query := m.collection.Find(bson.M{"_id": requestID})
	result := make(map[string]interface{})
	if err := query.One(&result); err == mgo.ErrNotFound {
		return nil, nil
	} else if err != nil {
		return nil, err
	} else {
		return &result, nil
	}
}
