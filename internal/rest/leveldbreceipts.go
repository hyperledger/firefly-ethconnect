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
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/kaleido-io/ethconnect/internal/errors"
	"github.com/kaleido-io/ethconnect/internal/kvstore"
	"github.com/oklog/ulid/v2"
	log "github.com/sirupsen/logrus"
	"github.com/syndtr/goleveldb/leveldb/util"
)

type levelDBReceipts struct {
	conf         *LevelDBReceiptStoreConf
	store        kvstore.KVStore
	idEntropy    *ulid.MonotonicEntropy
	defaultLimit int
}

func newLevelDBReceipts(conf *LevelDBReceiptStoreConf) (*levelDBReceipts, error) {
	store, err := kvstore.NewLDBKeyValueStore(conf.Path)
	if err != nil {
		return nil, errors.Errorf(errors.ReceiptStoreLevelDBConnect, err)
	}
	t := time.Unix(1000000, 0)
	entropy := ulid.Monotonic(rand.New(rand.NewSource(t.UnixNano())), 0)

	return &levelDBReceipts{
		conf:         conf,
		store:        store,
		idEntropy:    entropy,
		defaultLimit: 50,
	}, nil
}

// AddReceipt processes an individual reply message, and contains all errors
// To account for any transitory failures writing to mongoDB, it retries adding receipt with a backoff
func (l *levelDBReceipts) AddReceipt(requestID string, receipt *map[string]interface{}) (err error) {
	// insert an entry with a composite key to track the insertion order
	newId := ulid.MustNew(ulid.Timestamp(time.Now()), l.idEntropy)
	// add "z" prefix so these entries come after the lookup entries
	// because for iteration we start from last backwards
	lookupKey := fmt.Sprintf("z%s", newId)

	b, _ := json.MarshalIndent(receipt, "", "  ")
	l.store.Put(lookupKey, b)

	// build the index for "from"
	fromKey := fmt.Sprintf("from:%s:%s", (*receipt)["from"], lookupKey)
	l.store.Put(fromKey, []byte(lookupKey))

	// build the index for "to" if a value is present
	to := (*receipt)["to"]
	if to != "" {
		toKey := fmt.Sprintf("to:%s:%s", to, lookupKey)
		l.store.Put(toKey, []byte(lookupKey))
	}

	// build the indiex for "receivedAt"
	receivedAtKey := fmt.Sprintf("receivedAt:%d:%s", (*receipt)["receivedAt"], lookupKey)
	l.store.Put(receivedAtKey, []byte(lookupKey))

	// insert the lookup entry for GetReceipt()
	return l.store.Put(requestID, []byte(lookupKey))
}

// GetReceipts Returns recent receipts with skip, limit and other query parameters
func (l *levelDBReceipts) GetReceipts(skip, limit int, ids []string, sinceEpochMS int64, from, to, start string) (*[]map[string]interface{}, error) {
	// the application of the parameters are implemented to match mongo queries:
	// - find the starting point:
	//   - if "start" is present, use it
	//   - otherwise use "Last()"
	// - if "skip" is present, forward to the count
	// - if "ids" are present, use them to look up the specific entries and filter out the entries falling out of the cursor range
	// - if "from" or "to" are present, look up the entries using the "from:[address]" and "to:[address]" prefix then work out the intersection of the [lookupKey] segments
	var itr kvstore.KVIterator
	var endKey string
	results := []map[string]interface{}{}
	if sinceEpochMS > 0 {
		// locate the iterator range limit
		keyRange := l.findEndPoint(sinceEpochMS)
		if keyRange != nil {
			itr = l.store.NewIterator(keyRange)
			endKey = string(string(keyRange.Limit))
		} else {
			// no entries match the sinceEpochMS, return empty
			return &results, nil
		}
	} else {
		// create the iterator without a range
		itr = l.store.NewIterator()
	}
	defer itr.Release()

	if limit == 0 {
		limit = l.defaultLimit
	}

	// if we have reference points, like list of IDs and "from/to" parameters, process using the reference points
	var lookupKeys []string
	var lookupKeysByIDs []string
	if len(ids) > 0 {
		// use the list of IDs to search for the result entries
		lookupKeysByIDs = l.getLookupKeysByIDs(ids, start, endKey)
	}
	var lookupKeysByFromAndTo []string
	if from != "" || to != "" {
		lookupKeysByFromAndTo = l.getLookupKeysByFromAndTo(from, to, start, endKey)
	}
	if len(ids) > 0 && from == "" && to == "" {
		lookupKeys = lookupKeysByIDs
	} else if len(ids) == 0 && (from != "" || to != "") {
		lookupKeys = lookupKeysByFromAndTo
	} else if len(ids) > 0 && (from != "" || to != "") {
		lookupKeys = intersect(lookupKeysByIDs, lookupKeysByFromAndTo)
		results := l.getReceiptsByLookupKey(lookupKeys, limit)
		return results, nil
	}

	// no reference points, iterate normally
	index := 0
	var valid bool
	if start != "" {
		valid = itr.Seek(start)
	} else {
		valid = itr.Last()
	}
	for ; valid; valid = itr.Prev() {
		if limit > 0 && len(results) >= limit {
			break
		}

		if index >= skip {
			key := itr.Key()
			if !strings.HasPrefix(key, "z") {
				// we have iterated all the composite key entries
				break
			}
			val := itr.Value()
			receipt := make(map[string]interface{})
			err := json.Unmarshal(val, &receipt)
			if err != nil {
				log.Errorf("Failed to decode stored receipt for request ID %s\n", itr.Key())
				continue
			} else {
				receipt["_sequenceKey"] = key
				results = append(results, receipt)
			}
		}
		index++
	}
	return &results, nil
}

// getReply handles a HTTP request for an individual reply
func (l *levelDBReceipts) GetReceipt(requestID string) (*map[string]interface{}, error) {
	val, err := l.store.Get(requestID)
	if err != nil {
		if err.Error() == "leveldb: not found" {
			return nil, nil
		} else {
			log.Errorf("Failed to retrieve the entry for the original key: %s. %s\n", requestID, err)
			return nil, fmt.Errorf("Failed to the entry for the original key: %s. %s", requestID, err)
		}
	}
	// returned val represents the composite key, use it to retrieve the actual content
	lookupKey := string(val)
	content, err := l.store.Get(lookupKey)
	if err != nil {
		log.Errorf("Failed to retrieve the entry using the generated ID: %s. %s\n", lookupKey, err)
		return nil, fmt.Errorf("Failed to retrieve the entry using the generated ID: %s. %s", requestID, err)
	}

	result := make(map[string]interface{})
	err = json.Unmarshal(content, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (l *levelDBReceipts) findEndPoint(sinceEpochMS int64) *util.Range {
	searchKey := fmt.Sprintf("receivedAt:%d", sinceEpochMS)
	itr := l.store.NewIterator()
	defer itr.Release()
	found := itr.Seek(searchKey)
	if found {
		foundKey := itr.Key()
		log.Infof("Located first-hit entry for search key %s: %s", searchKey, foundKey)
		// must check if the found entry is a "receivedAt" index, because we may have gone past
		// all the "receiveAt" entries without a hit
		if strings.HasPrefix(foundKey, "receivedAt:") {
			segments := strings.Split(foundKey, ":")
			return &util.Range{
				Limit: []byte(segments[2]),
			}
		}
	}
	return nil
}

func (l *levelDBReceipts) getLookupKeysByIDs(ids []string, start, end string) []string {
	result := []string{}
	for _, id := range ids {
		val, err := l.store.Get(id)
		if err != nil {
			log.Warnf("Failed to locate entry for key ID %s", id)
			continue
		} else {
			result = append(result, string(val))
		}
	}
	sort.Sort(sort.StringSlice(result))
	// since our query searches in descending order, while sort.SearchString requires ascending order
	// the "start" is the end, and "end" is the start
	result = applyBound(result, end, start)
	return result
}

func (l *levelDBReceipts) getLookupKeysByFromAndTo(from, to string, start, end string) []string {
	if from != "" || to != "" {
		fromKeys := []string{}
		itr := l.store.NewIterator()
		defer itr.Release()

		if from != "" {
			searchKey := fmt.Sprintf("from:%s", from)
			found := itr.Seek(searchKey)
			if found {
				startKey := itr.Key()
				log.Infof("Located entry for 'from' address %s: %s", from, startKey)
				if strings.HasPrefix(startKey, searchKey) {
					val := itr.Value()
					fromKeys = append(fromKeys, string(val))
				}
				for itr.Next() {
					key := itr.Key()
					if strings.HasPrefix(key, searchKey) {
						val := itr.Value()
						fromKeys = append(fromKeys, string(val))
					} else {
						break
					}
				}
			}
		}

		toKeys := []string{}
		if to != "" {
			searchKey := fmt.Sprintf("to:%s", to)
			found := itr.Seek(searchKey)
			if found {
				startKey := itr.Key()
				log.Infof("Located entry for 'to' address %s: %s", to, startKey)
				if strings.HasPrefix(startKey, searchKey) {
					val := itr.Value()
					toKeys = append(toKeys, string(val))
				}
				for itr.Next() {
					key := itr.Key()
					if strings.HasPrefix(key, searchKey) {
						val := itr.Value()
						toKeys = append(toKeys, string(val))
					} else {
						break
					}
				}
			}
		}

		var result []string
		if from != "" && to == "" {
			result = fromKeys
		} else if from == "" && to != "" {
			result = toKeys
		} else {
			// find the intersection of the 2 slices, note that both slices are sorted
			result = intersect(fromKeys, toKeys)
		}
		// since our query searches in descending order, while sort.SearchString requires ascending order
		// the "start" is the end, and "end" is the start
		result = applyBound(result, end, start)
		return result
	}
	return nil
}

func (l *levelDBReceipts) getReceiptsByLookupKey(lookupKeys []string, limit int) *[]map[string]interface{} {
	length := len(lookupKeys)
	if limit > 0 && limit < length {
		length = limit
	}
	results := []map[string]interface{}{}
	i := 0
	for _, key := range lookupKeys {
		if i >= length {
			break
		}
		val, err := l.store.Get(key)
		if err != nil {
			log.Errorf("Failed to find entry for lookup key %s\n", key)
			continue
		}
		receipt := make(map[string]interface{})
		err = json.Unmarshal(val, &receipt)
		if err != nil {
			log.Errorf("Failed to decode stored receipt for lookup key %s\n", key)
			continue
		}
		results = append(results, receipt)
		i++
	}
	return &results
}

func intersect(arr1, arr2 []string) []string {
	set := []string{}
	for i := 0; i < len(arr1); i++ {
		from := arr1[i]
		idx := sort.SearchStrings(arr2, from)
		if idx < len(arr2) && arr2[idx] == from {
			set = append(set, from)
		}
	}
	return set
}

func applyBound(arr []string, start, end string) []string {
	// apply the start and end bound if present
	if start != "" {
		startPos := sort.SearchStrings(arr, start)
		arr = arr[startPos:]
	}
	if end != "" {
		endPos := sort.SearchStrings(arr, end)
		arr = arr[:endPos]
	}
	return arr
}
