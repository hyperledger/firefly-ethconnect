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

package kldkvstore

import (
	"github.com/syndtr/goleveldb/leveldb"
)

// MockKV simple memory K/V store for testing
type MockKV struct {
	KVS       map[string][]byte
	StoreErr  error
	LoadErr   error
	DeleteErr error
}

// Put a key
func (m *MockKV) Put(key string, val []byte) error {
	m.KVS[key] = val
	return m.StoreErr
}

// Get a key
func (m *MockKV) Get(key string) ([]byte, error) {
	v, exists := m.KVS[key]
	if m.LoadErr == nil && !exists {
		return nil, leveldb.ErrNotFound
	}
	return v, m.LoadErr
}

// Delete a key
func (m *MockKV) Delete(key string) error {
	delete(m.KVS, key)
	return m.DeleteErr
}

// NewIterator for a new iterator
func (m *MockKV) NewIterator() KVIterator {
	return nil // not implemented in mock
}

// Close it
func (m *MockKV) Close() {}

// NewMockKV constructor
func NewMockKV(err error) *MockKV {
	return &MockKV{
		StoreErr:  err,
		LoadErr:   err,
		DeleteErr: err,
		KVS:       make(map[string][]byte),
	}
}
