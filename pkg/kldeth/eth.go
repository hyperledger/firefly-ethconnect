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
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/kaleido-io/ethconnect/pkg/kldmessages"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	log "github.com/sirupsen/logrus"
)

// KldTx wraps an ethereum transaction, along with the logic to send it over
// JSON/RPC to a node
type KldTx struct {
	From  common.Address
	EthTX *types.Transaction
}

// NewContractDeployTxn builds a new ethereum transaction from the supplied
// SendTranasction message
func NewContractDeployTxn(msg kldmessages.DeployContract) (pTX *KldTx, err error) {

	// Compile the solidity contract
	compiledSolidity, err := CompileContract(msg.Solidity, msg.ContractName)
	if err != nil {
		return
	}

	// Build correctly typed args for the ethereum call
	typedArgs, err := msg.GenerateTypedArgs(compiledSolidity.ABI.Constructor)
	if err != nil {
		return
	}

	// Pack the arguments
	packedCall, err := compiledSolidity.ABI.Pack("", typedArgs...)
	if err != nil {
		err = fmt.Errorf("Packing arguments for constructor: %s", err)
		return
	}

	// Generate the ethereum transaction
	var tx KldTx
	tx.EthTX, tx.From, err = msg.ToEthTransaction(2, "", packedCall)
	if err != nil {
		return
	}
	pTX = &tx

	return
}

// Send sends an individual transaction, choosing external or internal signing
func (tx *KldTx) Send(rpc *rpc.Client) (string, error) {
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
func (tx *KldTx) sendUnsignedTxn(ctx context.Context, rpc *rpc.Client) (string, error) {
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
