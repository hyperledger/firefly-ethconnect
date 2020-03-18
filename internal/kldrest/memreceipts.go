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

package kldrest

import (
	"container/list"
	"sync"

	"github.com/kaleido-io/ethconnect/internal/klderrors"
	log "github.com/sirupsen/logrus"
)

type memoryReceipts struct {
	conf     *ReceiptStoreConf
	receipts *list.List
	mux      sync.Mutex
}

func newMemoryReceipts(conf *ReceiptStoreConf) *memoryReceipts {
	r := &memoryReceipts{
		conf:     conf,
		receipts: list.New(),
	}
	log.Debugf("Memory receipt store created, with MaxDocs=%d", r.conf.MaxDocs)
	return r
}

func (m *memoryReceipts) GetReceipts(skip, limit int, ids []string, sinceEpochMS int64, from, to string) (*[]map[string]interface{}, error) {
	m.mux.Lock()
	defer m.mux.Unlock()

	if len(ids) > 0 || sinceEpochMS != 0 || from != "" || to != "" {
		return nil, klderrors.Errorf(klderrors.KVStoreMemFilteringUnsupported)
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

func (m *memoryReceipts) GetReceipt(requestID string) (*map[string]interface{}, error) {
	m.mux.Lock()
	defer m.mux.Unlock()

	curElem := m.receipts.Front()
	for curElem != nil {
		r := *curElem.Value.(*map[string]interface{})
		id, exists := r["_id"]
		if exists && id == requestID {
			return &r, nil
		}
		curElem = curElem.Next()
	}
	return nil, nil
}

func (m *memoryReceipts) AddReceipt(receipt *map[string]interface{}) error {
	m.mux.Lock()
	defer m.mux.Unlock()

	curLen := m.receipts.Len()
	if curLen > 0 && curLen >= m.conf.MaxDocs {
		m.receipts.Remove(m.receipts.Back())
	}
	m.receipts.PushFront(receipt)
	return nil
}
