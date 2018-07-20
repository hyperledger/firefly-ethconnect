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
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/globalsign/mgo"
	"github.com/globalsign/mgo/bson"
	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldkafka"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	log "github.com/sirupsen/logrus"
)

// MongoDatabase is a subset of mgo that we use, allowing stubbing
type MongoDatabase interface {
	Connect(url string) error
	GetCollection(database string, collection string) MongoCollection
}

type mgoWrapper struct {
	session *mgo.Session
}

func (m *mgoWrapper) Connect(url string) (err error) {
	m.session, err = mgo.Dial(url)
	return
}

func (m *mgoWrapper) GetCollection(database string, collection string) MongoCollection {
	return &collWrapper{coll: m.session.DB(database).C(collection)}
}

// MongoCollection is the subset of mgo that we use, allowing stubbing
type MongoCollection interface {
	Insert(...interface{}) error
	Create(info *mgo.CollectionInfo) error
	EnsureIndex(index mgo.Index) error
	Find(query interface{}) MongoQuery
}

type collWrapper struct {
	coll *mgo.Collection
}

func (m *collWrapper) Insert(docs ...interface{}) error {
	return m.coll.Insert(docs)
}

func (m *collWrapper) Create(info *mgo.CollectionInfo) error {
	return m.coll.Create(info)
}

func (m *collWrapper) EnsureIndex(index mgo.Index) error {
	return m.coll.EnsureIndex(index)
}

func (m *collWrapper) Find(query interface{}) MongoQuery {
	return m.coll.Find(query)
}

// MongoQuery is the subset of mgo that we use, allowing stubbing
type MongoQuery interface {
	Limit(n int) *mgo.Query
	Skip(n int) *mgo.Query
	Sort(fields ...string) *mgo.Query
	All(result interface{}) error
	One(result interface{}) error
}

func (w *WebhooksBridge) connectMongoDB(mongo MongoDatabase) (err error) {
	if w.conf.MongoDB.URL == "" {
		log.Debugf("No MongoDB URL configured. Receipt store disabled")
		return
	}
	err = mongo.Connect(w.conf.MongoDB.URL)
	if err != nil {
		err = fmt.Errorf("Unable to connect to MongoDB: %s", err)
		return
	}
	w.mongo = mongo.GetCollection(w.conf.MongoDB.Database, w.conf.MongoDB.Collection)
	if collErr := w.mongo.Create(&mgo.CollectionInfo{
		Capped:  (w.conf.MongoDB.MaxDocs > 0),
		MaxDocs: w.conf.MongoDB.MaxDocs,
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
	if err = w.mongo.EnsureIndex(index); err != nil {
		err = fmt.Errorf("Unable to create index: %s", err)
		return
	}

	log.Infof("Connected to MongoDB on %s DB=%s Collection=%s", w.conf.MongoDB.URL, w.conf.MongoDB.Database, w.conf.MongoDB.Collection)
	return
}

// getString is a helper to safely extract strings from generic interface maps
func getString(genericMap map[string]interface{}, key string) string {
	log.Infof("genericMap: %+v", genericMap)
	if val, exists := genericMap[key]; exists {
		if reflect.TypeOf(val).Kind() == reflect.String {
			return val.(string)
		}
	}
	return ""
}

// processReply processes an individual reply message, and contains all errors
func (w *WebhooksBridge) processReply(msgBytes []byte) {

	// Parse the reply as JSON
	var parsedMsg map[string]interface{}
	if err := json.Unmarshal(msgBytes, &parsedMsg); err != nil {
		log.Errorf("Unable to unmarshal reply message '%s' as JSON: %s", string(msgBytes), err)
		return
	}

	// Extract the headers
	var headers map[string]interface{}
	if iHeaders, exists := parsedMsg["headers"]; exists && reflect.TypeOf(headers).Kind() == reflect.Map {
		headers = iHeaders.(map[string]interface{})
	} else {
		log.Errorf("Failed to extract request headers from '%s'", string(msgBytes))
		return
	}

	// The one field we require is the original ID (as it's the key in MongoDB)
	requestID := getString(headers, "requestId")
	if requestID == "" {
		log.Errorf("Failed to extract headers.requestId from '%s'", string(msgBytes))
		return
	}
	reqOffset := getString(headers, "reqOffset")
	msgType := getString(headers, "type")
	result := ""
	if msgType == kldmessages.MsgTypeError {
		result = getString(parsedMsg, "errorMessage")
	} else {
		result = getString(parsedMsg, "transactionHash")
	}
	log.Infof("Received reply message. requestId='%s' reqOffset='%s' type='%s': %s", requestID, reqOffset, msgType, result)

	// Insert the receipt into MongoDB
	if requestID != "" && w.mongo != nil {
		parsedMsg["receivedAt"] = time.Now().UnixNano() / int64(time.Millisecond)
		parsedMsg["_id"] = requestID
		if err := w.mongo.Insert(parsedMsg); err != nil {
			log.Errorf("Failed to insert '%s' into mongodb: %s", string(msgBytes), err)
		} else {
			log.Infof("Inserted receipt into MongoDB")
		}
	}
}

// ConsumerMessagesLoop - consume replies
func (w *WebhooksBridge) ConsumerMessagesLoop(consumer kldkafka.KafkaConsumer, producer kldkafka.KafkaProducer, wg *sync.WaitGroup) {
	for msg := range consumer.Messages() {
		w.processReply(msg.Value)
	}
	wg.Done()
}

// getReplies handles a HTTP request for recent replies
func (w *WebhooksBridge) getReplies(res http.ResponseWriter, req *http.Request, params httprouter.Params) {

	res.Header().Set("Content-Type", "application/json")
	if w.mongo == nil {
		errReply(res, fmt.Errorf("Receipt store not enabled"), 405)
		return
	}

	// Set skip and limit
	query := w.mongo.Find(bson.M{})
	query.Sort("-receivedAt")
	limit := 10 // default the limit
	log.Debugf("GET /replies: limit=%s skip=%s", req.FormValue("limit"), req.FormValue("skip"))
	if customLimit, err := strconv.ParseInt(req.FormValue("limit"), 10, 32); err == nil {
		if int(customLimit) > w.conf.MongoDB.QueryLimit {
			limit = w.conf.MongoDB.QueryLimit
		} else if customLimit > 0 {
			limit = int(customLimit)
		}
	} else {
		log.Warnf("Ignoring invalid limit %s: %s", req.FormValue("limit"), err)
	}
	query.Limit(limit)
	if skip, err := strconv.ParseInt(req.FormValue("skip"), 10, 32); err == nil && skip > 0 {
		query.Skip(int(skip))
	} else {
		log.Warnf("Ignoring invalid skip %s: %s", req.FormValue("skip"), err)
	}

	// Perform the query
	results := make([]map[string]interface{}, limit)
	if err := query.All(results); err == mgo.ErrNotFound {
		errReply(res, fmt.Errorf("No replies found"), 404)
		log.Infof("GET /replies: No replies found")
		return
	} else if err != nil {
		log.Errorf("GET /replies: Error querying replies: %s", err)
		errReply(res, fmt.Errorf("Error querying replies"), 500)
		return
	} else {
		log.Infof("GET /replies: %d replies found", len(results))
	}

	// Serialize and return
	resBytes, err := json.Marshal(results)
	if err != nil {
		log.Errorf("Error serializing replies: %s", err)
		errReply(res, fmt.Errorf("Error serializing replies"), 500)
		return
	}
	res.WriteHeader(200)
	res.Write(resBytes)

}

// getReply handles a HTTP request for an individual reply
func (w *WebhooksBridge) getReply(res http.ResponseWriter, req *http.Request, params httprouter.Params) {

	res.Header().Set("Content-Type", "application/json")
	if w.mongo == nil {
		errReply(res, fmt.Errorf("Receipt store not enabled"), 405)
		return
	}

	id := params.ByName("id")
	query := w.mongo.Find(bson.M{"_id": id})
	result := make(map[string]interface{})
	if err := query.One(result); err == mgo.ErrNotFound {
		errReply(res, fmt.Errorf("Reply not found"), 404)
		log.Infof("GET /reply/%s: Reply not found", id)
		return
	} else if err != nil {
		log.Errorf("GET /reply/%s: Error querying reply: %s", id, err)
		errReply(res, fmt.Errorf("Error querying reply"), 500)
		return
	} else {
		log.Infof("GET /reply/%s: Reply found", id)
	}

	// Serialize and return
	resBytes, err := json.Marshal(result)
	if err != nil {
		log.Errorf("Error serializing receipts: %s", err)
		errReply(res, fmt.Errorf("Error serializing receipts"), 500)
		return
	}
	res.WriteHeader(200)
	res.Write(resBytes)

}
