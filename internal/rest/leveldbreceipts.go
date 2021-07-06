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
	"regexp"
	"strings"

	"github.com/kaleido-io/ethconnect/internal/errors"
	"github.com/kaleido-io/ethconnect/internal/kvstore"
)

type levelDBReceipts struct {
	conf  *LevelDBReceiptStoreConf
	store kvstore.KVStore
}

func newLevelDBReceipts(conf *LevelDBReceiptStoreConf) (*levelDBReceipts, error) {
	store, err := kvstore.NewLDBKeyValueStore(conf.Path)
	if err != nil {
		return nil, errors.Errorf(errors.ReceiptStoreLevelDBConnect, err)
	}
	return &levelDBReceipts{
		conf:  conf,
		store: store,
	}, nil
}

// AddReceipt processes an individual reply message, and contains all errors
// To account for any transitory failures writing to mongoDB, it retries adding receipt with a backoff
func (l *levelDBReceipts) AddReceipt(requestID string, receipt *map[string]interface{}) (err error) {
	// insert an entry with a composite key to track the insertion order
	timeElapsed := ((*receipt)["headers"].(map[string]interface{}))["timeElapsed"].(float64)
	timeSeconds := (*receipt)["receivedAt"].(int64) + int64(timeElapsed)
	timeStringSeconds := fmt.Sprintf("%d", timeSeconds)
	timeStringNanoSeconds := fmt.Sprintf("%.9f", timeElapsed)
	timeStringNanoSeconds = timeStringNanoSeconds[strings.Index(timeStringNanoSeconds, "."):]
	timestamp := timeStringSeconds + timeStringNanoSeconds

	// add "z" prefix so these entries come after the actual entries
	// because for iteration we start from last backwards
	compositeKey := fmt.Sprintf("z%s:%s", timestamp, requestID)
	l.store.Put(compositeKey, nil)

	// inser the actual entry
	b, _ := json.MarshalIndent(receipt, "", "  ")
	return l.store.Put(requestID, b)
}

// GetReceipts Returns recent receipts with skip & limit
func (l *levelDBReceipts) GetReceipts(skip, limit int, ids []string, sinceEpochMS int64, from, to string) (*[]map[string]interface{}, error) {
	itr := l.store.NewIterator()
	defer itr.Release()
	results := make([]map[string]interface{}, 0, limit)
	index := 0
	var errResult error
	for valid := itr.Last(); valid; valid = itr.Prev() {
		if limit > 0 && len(results) >= limit {
			break
		}

		if index >= skip {
			val := itr.Value()
			if len(val) > 0 {
				// the index keys don't have values. if we have hit an entry with a value
				// then we have reached the end of the index keys
				break
			}
			// extract the request key from the index key
			r, _ := regexp.Compile("z[0-9.]+:(.+)")
			compositeKey := itr.Key()
			matches := r.FindStringSubmatch(compositeKey)
			var lookupKey string
			if len(matches) == 2 {
				lookupKey = matches[1]
			} else {
				fmt.Printf("Failed to parse composite key %s\n", compositeKey)
			}
			val, err := l.store.Get(lookupKey)
			if err != nil {
				fmt.Printf("Failed to locate record by key %s\n", lookupKey)
				continue
			}
			receipt := make(map[string]interface{})
			err = json.Unmarshal(val, &receipt)
			if err != nil {
				fmt.Printf("Failed to decode stored receipt for request %s. This record is skipped in the returned list", itr.Key())
				errResult = err
				continue
			} else {
				shouldInclude := true
				if len(ids) > 0 {
					found := false
					for _, v := range ids {
						if receipt["_id"].(string) == v {
							found = true
							break
						}
					}
					if !found {
						shouldInclude = false
					}
				}
				if sinceEpochMS > 0 && int64(receipt["receivedAt"].(float64)) <= sinceEpochMS {
					shouldInclude = false
				}
				if from != "" && receipt["from"] != from {
					shouldInclude = false
				}
				if to != "" && receipt["to"] != to {
					shouldInclude = false
				}
				if shouldInclude {
					results = append(results, receipt)
				}
			}
		}
		index++
	}
	if errResult != nil {
		return nil, errResult
	}
	return &results, nil
}

// getReply handles a HTTP request for an individual reply
func (l *levelDBReceipts) GetReceipt(requestID string) (*map[string]interface{}, error) {
	result := make(map[string]interface{})
	val, err := l.store.Get(requestID)
	if err != nil {
		if err.Error() == "leveldb: not found" {
			return nil, nil
		} else {
			fmt.Printf("Failed to get value for request %s: %s\n", requestID, err)
			return nil, fmt.Errorf("Failed to get value for request %s: %s", requestID, err)
		}
	}
	err = json.Unmarshal(val, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
