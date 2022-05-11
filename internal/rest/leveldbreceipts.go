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
	"sync"
	"time"

	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/kvstore"
	"github.com/oklog/ulid/v2"
	log "github.com/sirupsen/logrus"
	"github.com/syndtr/goleveldb/leveldb/util"
)

type levelDBReceipts struct {
	conf         *LevelDBReceiptStoreConf
	store        kvstore.KVStore
	entropyLock  sync.Mutex
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
		defaultLimit: conf.QueryLimit,
	}, nil
}

// AddReceipt processes an individual reply message, and contains all errors
// To account for any transitory failures writing to mongoDB, it retries adding receipt with a backoff
func (l *levelDBReceipts) AddReceipt(requestID string, receipt *map[string]interface{}, overwrite bool) (err error) {
	// insert an entry with a composite key to track the insertion order
	l.entropyLock.Lock()
	newID := ulid.MustNew(ulid.Timestamp(time.Now()), l.idEntropy)
	// We check for key uniqueness in this lock, to provide an idempotent interface
	existingKey, err := l.store.Get(requestID)
	l.entropyLock.Unlock()

	lookupKey := fmt.Sprintf("z%s", newID)
	if err == kvstore.ErrorNotFound {
		// This is what we expect
		err = nil
	} else if err == nil {
		if overwrite {
			lookupKey = string(existingKey)
		} else {
			return errors.Errorf(errors.ReceiptStoreLevelDBKeyNotUnique)
		}
	}

	// add "z" prefix so these entries come after the lookup entries
	// because for iteration we start from last backwards
	b, _ := json.MarshalIndent(receipt, "", "  ")
	err = l.store.Put(lookupKey, b)

	if err == nil {
		// build the index for "from"
		fromKey := fmt.Sprintf("from:%s:%s", (*receipt)["from"], lookupKey)
		err = l.store.Put(fromKey, []byte(lookupKey))
	}

	if err == nil {
		// build the index for "to" if a value is present
		to, ok := (*receipt)["to"]
		if ok && to != "" {
			toKey := fmt.Sprintf("to:%s:%s", to, lookupKey)
			err = l.store.Put(toKey, []byte(lookupKey))
		}
	}

	if err == nil {
		// build the index for "receivedAt"
		receivedAtKey := fmt.Sprintf("receivedAt:%d:%s", (*receipt)["receivedAt"], lookupKey)
		err = l.store.Put(receivedAtKey, []byte(lookupKey))
	}

	if err == nil {
		// insert the lookup entry for GetReceipt()
		err = l.store.Put(requestID, []byte(lookupKey))
	}
	return err
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
	var endKey string
	if sinceEpochMS > 0 {
		// locate the iterator range limit
		endKey = l.findEndPoint(sinceEpochMS)
		if endKey == "" {
			// no entries match the sinceEpochMS, return empty
			return &[]map[string]interface{}{}, nil
		}
	}
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
		lookupKeysByFromAndTo = l.getLookupKeysByFromAndTo(from, to, start, endKey, limit)
	}
	if len(ids) > 0 && from == "" && to == "" {
		lookupKeys = lookupKeysByIDs
	} else if len(ids) == 0 && (from != "" || to != "") {
		lookupKeys = lookupKeysByFromAndTo
	} else if len(ids) > 0 && (from != "" || to != "") {
		lookupKeys = intersect(lookupKeysByIDs, lookupKeysByFromAndTo)
	}
	if lookupKeys != nil {
		sort.Sort(sort.Reverse(sort.StringSlice(lookupKeys)))
		results := l.getReceiptsByLookupKey(lookupKeys, limit)
		return results, nil
	}

	// no reference points, iterate normally
	var itr kvstore.KVIterator
	if endKey != "" {
		// We iterate in reverse order, so the end key is the start
		itr = l.store.NewIteratorWithRange(&util.Range{
			Start: []byte(endKey),
		})
	} else {
		// create the iterator without a range
		itr = l.store.NewIterator()
	}
	defer itr.Release()

	results := l.getReceiptsNoFilter(itr, skip, limit, start)
	return &results, nil
}

func (l *levelDBReceipts) getReceiptsNoFilter(itr kvstore.KVIterator, skip, limit int, start string) []map[string]interface{} {
	results := []map[string]interface{}{}
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
	return results
}

// getReply handles a HTTP request for an individual reply
func (l *levelDBReceipts) GetReceipt(requestID string) (*map[string]interface{}, error) {
	val, err := l.store.Get(requestID)
	if err != nil {
		if err == kvstore.ErrorNotFound {
			return nil, nil
		} else {
			log.Errorf("Failed to retrieve the entry for the original key: %s. %s\n", requestID, err)
			return nil, errors.Errorf(errors.LevelDBFailedRetriveOriginalKey, requestID, err)
		}
	}
	// returned val represents the composite key, use it to retrieve the actual content
	lookupKey := string(val)
	content, err := l.store.Get(lookupKey)
	if err != nil {
		log.Errorf("Failed to retrieve the entry using the generated ID: %s. %s\n", lookupKey, err)
		return nil, errors.Errorf(errors.LevelDBFailedRetriveGeneratedID, requestID, err)
	}

	result := make(map[string]interface{})
	err = json.Unmarshal(content, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (l *levelDBReceipts) findEndPoint(sinceEpochMS int64) string {
	searchKey := fmt.Sprintf("receivedAt:%d:", sinceEpochMS)
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
			return segments[2]
		}
	}
	return ""
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
	sort.Strings(result)
	// since our query searches in descending order, while sort.SearchString requires ascending order
	// the "start" is the end, and "end" is the start
	result = applyBound(result, end, start)
	return result
}

func (l *levelDBReceipts) getLookupKeysByFromAndTo(from, to string, start, end string, limit int) []string {

	var fromKeys []string
	itr := l.store.NewIterator()
	defer itr.Release()

	if from != "" {
		searchKey := fmt.Sprintf("from:%s:", from)
		fromKeys = l.getLookupKeysByPrefix(itr, searchKey, limit)
	}

	var toKeys []string
	if to != "" {
		searchKey := fmt.Sprintf("to:%s:", to)
		toKeys = l.getLookupKeysByPrefix(itr, searchKey, limit)
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

func (l *levelDBReceipts) getLookupKeysByPrefix(itr kvstore.KVIterator, prefix string, limit int) []string {
	lookupKeys := []string{}
	count := 0
	found := itr.Seek(prefix)
	if found {
		startKey := itr.Key()
		log.Infof("Located entry for prefix %s: %s", prefix, startKey)
		if strings.HasPrefix(startKey, prefix) {
			val := itr.Value()
			lookupKeys = append(lookupKeys, string(val))
			count++
		}
		for itr.Next() {
			if count < limit {
				key := itr.Key()
				if strings.HasPrefix(key, prefix) {
					val := itr.Value()
					lookupKeys = append(lookupKeys, string(val))
				} else {
					break
				}
			} else {
				break
			}
			count++
		}
	}
	return lookupKeys
}

func (l *levelDBReceipts) getReceiptsByLookupKey(lookupKeys []string, limit int) *[]map[string]interface{} {
	length := len(lookupKeys)
	if limit > 0 && limit < length {
		length = limit
	}
	results := []map[string]interface{}{}
	for i, key := range lookupKeys {
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
