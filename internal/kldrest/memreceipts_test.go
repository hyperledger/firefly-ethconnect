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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemReceiptsWrapping(t *testing.T) {
	assert := assert.New(t)

	conf := &ReceiptStoreConf{
		MaxDocs: 50,
	}
	r := newMemoryReceipts(conf)

	for i := 0; i < 100; i++ {
		receipt := make(map[string]interface{})
		receipt["key"] = fmt.Sprintf("receipt_%d", i)
		r.AddReceipt(&receipt)
	}

	assert.Equal(50, r.receipts.Len())
	currItem := r.receipts.Front()
	for i := 0; i < 50; i++ {
		expectedKey := fmt.Sprintf("receipt_%d", 100-i-1)
		val := *currItem.Value.(*map[string]interface{})
		assert.Equal(val["key"], expectedKey)
		currItem = currItem.Next()
	}

	return
}

func TestMemReceiptsNoIDFilterImpl(t *testing.T) {
	assert := assert.New(t)

	conf := &ReceiptStoreConf{
		MaxDocs: 50,
	}
	r := newMemoryReceipts(conf)

	_, err := r.GetReceipts(0, 0, []string{"test"}, 0, "t", "t")
	assert.EqualError(err, "Memory receipts do not support filtering")

	return
}
