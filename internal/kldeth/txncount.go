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

	"github.com/ethereum/go-ethereum/common"
	log "github.com/sirupsen/logrus"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
)

// GetOrionTXCount uses the special Pantheon/Orion interface to check the
// next nonce for the privacy group associated with the privateFrom/privateFor combination
func GetOrionTXCount(ctx context.Context, rpc RPCClient, addr *common.Address, privacyGroup string) (int64, error) {
	start := time.Now().UTC()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var txnCount hexutil.Uint64
	if err := rpc.CallContext(ctx, &txnCount, "priv_getTransactionCount", addr, privacyGroup); err != nil {
		return 0, klderrors.Errorf(klderrors.TransactionSendNonceFailWithPrivacyGroup, privacyGroup, err)
	}
	callTime := time.Now().UTC().Sub(start)
	log.Debugf("priv_getTransactionCount(%x,%s)=%d [%.2fs]", addr, privacyGroup, txnCount, callTime.Seconds())
	log.Infof("Addr=%s PrivacyGroup=%s Nonce=%d", addr.String(), privacyGroup, txnCount)
	return int64(txnCount), nil
}

// GetTransactionCount gets the transaction count for an address
func GetTransactionCount(ctx context.Context, rpc RPCClient, addr *common.Address, blockNumber string) (int64, error) {
	start := time.Now().UTC()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var txnCount hexutil.Uint64
	if err := rpc.CallContext(ctx, &txnCount, "eth_getTransactionCount", addr, blockNumber); err != nil {
		return 0, klderrors.Errorf(klderrors.RPCCallReturnedError, "eth_getTransactionCount", err)
	}
	callTime := time.Now().UTC().Sub(start)
	log.Debugf("eth_getTransactionCount(%x,latest)=%d [%.2fs]", addr, txnCount, callTime.Seconds())
	return int64(txnCount), nil
}
