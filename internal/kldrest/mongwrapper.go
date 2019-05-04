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

// github.com/globalsign/mgo seems to be the most popular mgo fork. It seems
// a key feature we need is missing from the official Golang library.
// Specifically creation of collection with MaxDocuments set.
// We currently accept a lack of coverage on this file, as the use of struct
// (rather than interfaces) in the mgo library itself prevents stubbing.

import (
	"time"

	"github.com/globalsign/mgo"
)

// MongoDatabase is a subset of mgo that we use, allowing stubbing.
type MongoDatabase interface {
	Connect(url string, timeout time.Duration) error
	GetCollection(database string, collection string) MongoCollection
}

// MongoCollection is the subset of mgo that we use, allowing stubbing
type MongoCollection interface {
	Insert(...interface{}) error
	Create(info *mgo.CollectionInfo) error
	EnsureIndex(index mgo.Index) error
	Find(query interface{}) MongoQuery
}

type mgoWrapper struct {
	session *mgo.Session
}

func (m *mgoWrapper) Connect(url string, timeout time.Duration) (err error) {
	m.session, err = mgo.DialWithTimeout(url, timeout)
	return
}

func (m *mgoWrapper) GetCollection(database string, collection string) MongoCollection {
	return &collWrapper{coll: m.session.DB(database).C(collection)}
}

type collWrapper struct {
	coll *mgo.Collection
}

func (m *collWrapper) Insert(docs ...interface{}) error {
	return m.coll.Insert(docs...)
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
