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

	"github.com/ethereum/go-ethereum/common/hexutil"
	log "github.com/sirupsen/logrus"
)

// Send sends an individual transaction, choosing external or internal signing
func (tx *Txn) Send(rpc RPCClient) error {
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var err error
	// if tx.From == "" {
	// 	tx.Hash, err = w.signAndSendTxn(ctx, tx)
	// } else {
	tx.Hash, err = tx.sendUnsignedTxn(ctx, rpc)
	// }
	callTime := time.Now().Sub(start)
	if err != nil {
		log.Warnf("TX:%s Failed to send: %s [%.2fs]", tx.Hash, err, callTime.Seconds())
	} else {
		log.Infof("TX:%s Sent OK [%.2fs]", tx.Hash, callTime.Seconds())
	}
	return err
}

type sendTxArgs struct {
	Nonce    *hexutil.Uint64 `json:"nonce,omitempty"`
	From     string          `json:"from"`
	To       string          `json:"to,omitempty"`
	Gas      hexutil.Uint64  `json:"gas"`
	GasPrice hexutil.Big     `json:"gasPrice"`
	Value    hexutil.Big     `json:"value"`
	Data     *hexutil.Bytes  `json:"data"`
	// EEA spec extensions
	PrivateFrom string   `json:"privateFrom,omitempty"`
	PrivateFor  []string `json:"privateFor,omitempty"`
}

// sendUnsignedTxn sends a transaction for internal signing by the node
func (tx *Txn) sendUnsignedTxn(ctx context.Context, rpc RPCClient) (string, error) {
	data := hexutil.Bytes(tx.EthTX.Data())
	var nonce *hexutil.Uint64
	if !tx.NodeAssignNonce {
		hexNonce := hexutil.Uint64(tx.EthTX.Nonce())
		nonce = &hexNonce
	}
	args := sendTxArgs{
		Nonce:    nonce,
		From:     tx.From.Hex(),
		Gas:      hexutil.Uint64(tx.EthTX.Gas()),
		GasPrice: hexutil.Big(*tx.EthTX.GasPrice()),
		Value:    hexutil.Big(*tx.EthTX.Value()),
		Data:     &data,
	}
	// if tx.PrivateFrom != "" {
	// 	args.PrivateFrom = tx.PrivateFrom
	// 	args.PrivateFor = tx.PrivateFor
	// }
	var to = tx.EthTX.To()
	if to != nil {
		args.To = to.Hex()
	}
	var txHash string
	err := rpc.CallContext(ctx, &txHash, "eth_sendTransaction", args)
	return txHash, err
}
