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
func (tx *Txn) Send(rpc rpcClient) (string, error) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var err error
	var txHash string
	// if tx.From == "" {
	// 	txHash, err = w.signAndSendTxn(ctx, tx)
	// } else {
	txHash, err = tx.sendUnsignedTxn(ctx, rpc)
	// }
	callTime := time.Now().Sub(start)
	ok := (err == nil)
	log.Infof("TX:%s Sent. OK=%t [%.2fs]", txHash, ok, callTime.Seconds())
	return txHash, err
}

type sendTxArgs struct {
	Nonce    hexutil.Uint64 `json:"nonce"`
	From     string         `json:"from"`
	To       string         `json:"to,omitempty"`
	Gas      hexutil.Uint64 `json:"gas"`
	GasPrice hexutil.Big    `json:"gasPrice"`
	Value    hexutil.Big    `json:"value"`
	Data     *hexutil.Bytes `json:"data"`
	// EEA spec extensions
	PrivateFrom string   `json:"privateFrom,omitempty"`
	PrivateFor  []string `json:"privateFor,omitempty"`
}

// sendUnsignedTxn sends a transaction for internal signing by the node
func (tx *Txn) sendUnsignedTxn(ctx context.Context, rpc rpcClient) (string, error) {
	data := hexutil.Bytes(tx.EthTX.Data())
	args := sendTxArgs{
		Nonce:    hexutil.Uint64(tx.EthTX.Nonce()),
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
