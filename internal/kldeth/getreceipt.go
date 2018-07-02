// Copyright 2018 Kaleido, a ConsenSys business

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldeth

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
)

// GetTXReceipt gets the receipt for the transaction
func (tx *Txn) GetTXReceipt(rpc RPCClient) (bool, error) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := rpc.CallContext(ctx, &tx.Receipt, "eth_getTransactionReceipt", tx.Hash); err != nil {
		return false, err
	}
	callTime := time.Now().Sub(start)
	isMined := tx.Receipt.BlockNumber != nil && tx.Receipt.BlockNumber.ToInt().Uint64() > 0
	log.Debugf("eth_getTransactionReceipt(%x,latest)=%t [%.2fs]", tx.Hash, isMined, callTime.Seconds())
	return isMined, nil
}
