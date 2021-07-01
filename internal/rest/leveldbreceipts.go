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
	b, _ := json.MarshalIndent(&receipt, "", "  ")
	return l.store.Put(requestID, b)
}

// GetReceipts Returns recent receipts with skip & limit
func (l *levelDBReceipts) GetReceipts(skip, limit int, ids []string, sinceEpochMS int64, from, to string) (*[]map[string]interface{}, error) {
	itr := l.store.NewIterator()
	defer itr.Release()
	results := make([]map[string]interface{}, 0, limit)
	index := 0
	var errResult error
	for itr.Next() && len(results) < limit {
		if index >= skip {
			val := itr.Value()
			receipt := make(map[string]interface{})
			err := json.Unmarshal(val, &receipt)
			if err != nil {
				fmt.Printf("Failed to decode stored receipt for request %s. This record is skipped in the returned list", itr.Key())
				errResult = err
				break
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
