// Copyright 2019 Kaleido

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
	"container/list"
	"sync"

	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	log "github.com/sirupsen/logrus"
)

type MemoryReceipts struct {
	conf     *ReceiptStoreConf
	receipts *list.List
	byID     map[string]*map[string]interface{}
	mux      sync.Mutex
}

func NewMemoryReceipts(conf *ReceiptStoreConf) *MemoryReceipts {
	r := &MemoryReceipts{
		conf:     conf,
		receipts: list.New(),
		byID:     make(map[string]*map[string]interface{}),
	}
	log.Debugf("Memory receipt store created, with MaxDocs=%d", r.conf.MaxDocs)
	return r
}

func (m *MemoryReceipts) Receipts() *list.List {
	return m.receipts
}

func (m *MemoryReceipts) GetReceipts(skip, limit int, ids []string, sinceEpochMS int64, from, to, start string) (*[]map[string]interface{}, error) {
	m.mux.Lock()
	defer m.mux.Unlock()

	if len(ids) > 0 || sinceEpochMS != 0 || from != "" || to != "" {
		return nil, errors.Errorf(errors.KVStoreMemFilteringUnsupported)
	}

	results := make([]map[string]interface{}, 0, limit)
	curElem := m.receipts.Front()
	for i := 0; i < skip && curElem != nil; i++ {
		curElem = curElem.Next()
	}
	for i := 0; i < limit && curElem != nil; i++ {
		results = append(results, *curElem.Value.(*map[string]interface{}))
		curElem = curElem.Next()
	}
	return &results, nil
}

func (m *MemoryReceipts) GetReceipt(requestID string) (*map[string]interface{}, error) {
	m.mux.Lock()
	defer m.mux.Unlock()

	receipt, exists := m.byID[requestID]
	if exists {
		return receipt, nil
	}
	return nil, nil
}

func (m *MemoryReceipts) AddReceipt(requestID string, receipt *map[string]interface{}, overwrite bool) error {
	m.mux.Lock()
	defer m.mux.Unlock()

	curLen := m.receipts.Len()
	if curLen > 0 && curLen >= m.conf.MaxDocs {
		back := m.receipts.Back()
		if back != nil {
			receipt := *(back.Value.(*map[string]interface{}))
			existingID, ok := receipt["_id"]
			if ok {
				delete(m.byID, existingID.(string))
			}
			m.receipts.Remove(back)
		}
	}
	m.receipts.PushFront(receipt)
	m.byID[requestID] = receipt
	return nil
}
