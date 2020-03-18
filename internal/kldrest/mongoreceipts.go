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
	"time"

	"github.com/globalsign/mgo"
	"github.com/globalsign/mgo/bson"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	log "github.com/sirupsen/logrus"
)

const (
	mongoConnectTimeout = 10 * 1000
)

type mongoReceipts struct {
	conf       *MongoDBReceiptStoreConf
	mgo        MongoDatabase
	collection MongoCollection
}

func newMongoReceipts(conf *MongoDBReceiptStoreConf) *mongoReceipts {
	return &mongoReceipts{
		conf: conf,
		mgo:  &mgoWrapper{},
	}
}

func (m *mongoReceipts) connect() (err error) {
	if m.conf.ConnectTimeoutMS <= 0 {
		m.conf.ConnectTimeoutMS = mongoConnectTimeout
	}
	err = m.mgo.Connect(m.conf.URL, time.Duration(m.conf.ConnectTimeoutMS)*time.Millisecond)
	if err != nil {
		err = klderrors.Errorf(klderrors.ReceiptStoreMongoDBConnect, err)
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
		err = klderrors.Errorf(klderrors.ReceiptStoreMongoDBIndex, err)
		return
	}

	log.Infof("Connected to MongoDB on %s DB=%s Collection=%s", m.conf.URL, m.conf.Database, m.conf.Collection)
	return
}

// AddReceipt processes an individual reply message, and contains all errors
func (m *mongoReceipts) AddReceipt(receipt *map[string]interface{}) error {
	if err := m.collection.Insert(*receipt); err != nil {
		log.Errorf("Insert failed for object '%.v': %s", *receipt, err)
		return err
	}
	return nil
}

// GetReceipts Returns recent receipts with skip & limit
func (m *mongoReceipts) GetReceipts(skip, limit int, ids []string, sinceEpochMS int64, from, to string) (*[]map[string]interface{}, error) {
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
func (m *mongoReceipts) GetReceipt(requestID string) (*map[string]interface{}, error) {
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
