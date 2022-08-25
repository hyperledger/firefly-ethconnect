// Copyright 2022 Kaleido

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

// ReceiptStorePersistence interface implemented by persistence layers
type ReceiptStorePersistence interface {
	GetReceipts(skip, limit int, ids []string, sinceEpochMS int64, from, to, start string) (*[]map[string]interface{}, error)
	GetReceipt(requestID string) (*map[string]interface{}, error)
	AddReceipt(requestID string, receipt *map[string]interface{}, overwriteAndRetry bool) error
}

// ReceiptStoreConf is the common configuration for all receipt stores
type ReceiptStoreConf struct {
	MaxDocs             int `json:"maxDocs"`
	QueryLimit          int `json:"queryLimit"`
	RetryInitialDelayMS int `json:"retryInitialDelay"`
	RetryTimeoutMS      int `json:"retryTimeout"`
}

// MongoDBReceiptStoreConf is the configuration for a MongoDB receipt store
type MongoDBReceiptStoreConf struct {
	ReceiptStoreConf
	URL              string `json:"url"`
	Database         string `json:"database"`
	Collection       string `json:"collection"`
	ConnectTimeoutMS int    `json:"connectTimeout"`
}

// LevelDBReceiptStoreConf is the configuration for a LevelDB receipt store
type LevelDBReceiptStoreConf struct {
	ReceiptStoreConf
	Path string `json:"path"`
}
