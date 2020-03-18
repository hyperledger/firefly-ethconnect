// Copyright 2018, 2019 Kaleido

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

	"github.com/kaleido-io/ethconnect/internal/klderrors"
	log "github.com/sirupsen/logrus"
)

// GetTXReceipt gets the receipt for the transaction
func (tx *Txn) GetTXReceipt(ctx context.Context, rpc RPCClient) (bool, error) {
	start := time.Now().UTC()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := rpc.CallContext(ctx, &tx.Receipt, "eth_getTransactionReceipt", tx.Hash); err != nil {
		return false, klderrors.Errorf(klderrors.RPCCallReturnedError, "eth_getTransactionReceipt", err)
	}
	callTime := time.Now().UTC().Sub(start)
	isMined := tx.Receipt.BlockNumber != nil && tx.Receipt.BlockNumber.ToInt().Uint64() > 0
	log.Debugf("eth_getTransactionReceipt(%x,latest)=%t [%.2fs]", tx.Hash, isMined, callTime.Seconds())

	if tx.PrivacyGroupID != "" {
		// priv_getTransactionReceipt expects the txHash and the public key of enclave (privateFrom)
		if err := rpc.CallContext(ctx, &tx.Receipt, "priv_getTransactionReceipt", tx.Hash, tx.PrivateFrom); err != nil {
			return false, klderrors.Errorf(klderrors.RPCCallReturnedError, "priv_getTransactionReceipt", err)
		}
	}

	return isMined, nil
}
