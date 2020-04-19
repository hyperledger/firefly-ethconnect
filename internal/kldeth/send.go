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
	"encoding/hex"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	log "github.com/sirupsen/logrus"
)

const (
	errorFunctionSelector = "0x08c379a0" // per https://solidity.readthedocs.io/en/v0.4.24/control-structures.html the signature of Error(string)
)

// calculateGas uses eth_estimateGas to estimate the gas required, providing a buffer
// of 20% for variation as the chain changes between estimation and submission.
func (tx *Txn) calculateGas(ctx context.Context, rpc RPCClient, txArgs *SendTXArgs, gas *hexutil.Uint64) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := rpc.CallContext(ctx, &gas, "eth_estimateGas", txArgs); err != nil {
		// Now we attempt a call of the transaction, because that will return us a useful error in the case, of a revert.
		estError := klderrors.Errorf(klderrors.TransactionSendGasEstimateFailed, err)
		log.Errorf(estError.Error())
		if _, err := tx.Call(ctx, rpc, "latest"); err != nil {
			return err
		}
		// If the call succeeds, after estimate completed - we still need to fail with the estimate error
		return estError
	}
	*gas = hexutil.Uint64(float64(*gas) * 1.2)
	return nil
}

// Call synchronously calls the method, without mining a transaction, and returns the result as RLP encoded bytes or nil
func (tx *Txn) Call(ctx context.Context, rpc RPCClient, blocknumber string) (res []byte, err error) {
	data := hexutil.Bytes(tx.EthTX.Data())
	txArgs := &SendTXArgs{
		From:     tx.From.Hex(),
		GasPrice: hexutil.Big(*tx.EthTX.GasPrice()),
		Value:    hexutil.Big(*tx.EthTX.Value()),
		Data:     &data,
	}
	var to = tx.EthTX.To()
	if to != nil {
		txArgs.To = to.Hex()
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var hexString string
	if err = rpc.CallContext(ctx, &hexString, "eth_call", txArgs, blocknumber); err != nil {
		return nil, klderrors.Errorf(klderrors.TransactionSendCallFailedNoRevert, err)
	}
	if len(hexString) == 0 || hexString == "0x" {
		return nil, nil
	}
	retStrLen := uint64(len(hexString))
	if strings.HasPrefix(hexString, errorFunctionSelector) && retStrLen > 138 {
		// The call reverted. Process the error response
		dataOffsetHex := new(big.Int)
		dataOffsetHex.SetString(hexString[10:74], 16)
		errorStringLen := new(big.Int)
		errorStringLen.SetString(hexString[74:138], 16)
		hexStringEnd := errorStringLen.Uint64()*2 + 138
		if hexStringEnd > retStrLen {
			hexStringEnd = retStrLen
		}
		errorStringHex := hexString[138:hexStringEnd]
		errorStringBytes, err := hex.DecodeString(errorStringHex)
		log.Warnf("EVM Reverted. Message='%s' Offset='%s'", errorStringBytes, dataOffsetHex.Text(10))
		if err != nil {
			return nil, klderrors.Errorf(klderrors.TransactionSendCallFailedRevertNoMessage)
		}
		return nil, klderrors.Errorf(klderrors.TransactionSendCallFailedRevertMessage, errorStringBytes)
	}
	log.Debugf("eth_call response: %s", hexString)
	res = common.FromHex(hexString)
	return
}

// Send sends an individual transaction, choosing external or internal signing
func (tx *Txn) Send(ctx context.Context, rpc RPCClient) (err error) {
	start := time.Now().UTC()

	gas := hexutil.Uint64(tx.EthTX.Gas())
	data := hexutil.Bytes(tx.EthTX.Data())
	txArgs := &SendTXArgs{
		From:     tx.From.Hex(),
		GasPrice: hexutil.Big(*tx.EthTX.GasPrice()),
		Value:    hexutil.Big(*tx.EthTX.Value()),
		Data:     &data,
	}
	var to = tx.EthTX.To()
	if to != nil {
		txArgs.To = to.Hex()
	}
	if uint64(gas) == uint64(0) {
		if err = tx.calculateGas(ctx, rpc, txArgs, &gas); err != nil {
			return
		}
		// Re-encode the EthTX (for external HD Wallet signing)
		if to != nil {
			tx.EthTX = types.NewTransaction(tx.EthTX.Nonce(), *tx.EthTX.To(), tx.EthTX.Value(), uint64(gas), tx.EthTX.GasPrice(), tx.EthTX.Data())
		} else {
			tx.EthTX = types.NewContractCreation(tx.EthTX.Nonce(), tx.EthTX.Value(), uint64(gas), tx.EthTX.GasPrice(), tx.EthTX.Data())
		}
	}
	txArgs.Gas = &gas

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tx.Hash, err = tx.submitTXtoNode(ctx, rpc, txArgs)

	callTime := time.Now().UTC().Sub(start)
	if err != nil {
		log.Warnf("TX:%s Failed to send: %s [%.2fs]", tx.Hash, err, callTime.Seconds())
	} else {
		log.Infof("TX:%s Sent OK [%.2fs]", tx.Hash, callTime.Seconds())
	}
	return err
}

// SendTXArgs is the JSON arguments that can be passed to an eth_sendTransaction call,
// and also the interface passed to the signer in the case of pre-signing
type SendTXArgs struct {
	Nonce    *hexutil.Uint64 `json:"nonce,omitempty"`
	From     string          `json:"from"`
	To       string          `json:"to,omitempty"`
	Gas      *hexutil.Uint64 `json:"gas,omitempty"`
	GasPrice hexutil.Big     `json:"gasPrice,omitempty"`
	Value    hexutil.Big     `json:"value,omitempty"`
	Data     *hexutil.Bytes  `json:"data"`
	// EEA spec extensions
	PrivateFrom    string   `json:"privateFrom,omitempty"`
	PrivateFor     []string `json:"privateFor,omitempty"`
	PrivacyGroupID string   `json:"privacyGroupId,omitempty"`
	Restriction    string   `json:"restriction,omitempty"`
}

// submitTXtoNode sends a transaction
// - If no signer interface: For internal signing by the node
// - If a signer interface is present: Pre-signed by this process
func (tx *Txn) submitTXtoNode(ctx context.Context, rpc RPCClient, txArgs *SendTXArgs) (string, error) {
	var nonce *hexutil.Uint64
	if !tx.NodeAssignNonce {
		hexNonce := hexutil.Uint64(tx.EthTX.Nonce())
		nonce = &hexNonce
	}
	txArgs.Nonce = nonce
	isPrivate := false
	jsonRPCMethod := "eth_sendTransaction"
	if tx.PrivacyGroupID != "" {
		// This means we have an Orion style private TX.
		// Earlier logic for Orion resolved the privateFor array down to a privacyGroupId
		jsonRPCMethod = "eea_sendTransaction"
		txArgs.PrivateFrom = tx.PrivateFrom
		txArgs.PrivacyGroupID = tx.PrivacyGroupID
		txArgs.Restriction = "restricted"
		// PrivateFrom is requires for Orion transactions
		if txArgs.PrivateFrom == "" {
			return "", klderrors.Errorf(klderrors.TransactionSendMissingPrivateFromOrion)
		}
		isPrivate = true
	} else if len(tx.PrivateFor) > 0 {
		// Note that PrivateFrom is optional for Quorum/Tessera transactions
		txArgs.PrivateFrom = tx.PrivateFrom
		txArgs.PrivateFor = tx.PrivateFor
		isPrivate = true
	}

	var callParam0 interface{} = txArgs
	if tx.Signer != nil {
		if isPrivate {
			return "", klderrors.Errorf(klderrors.TransactionSendPrivateTXWithExternalSigner, tx.Signer.Type())
		}
		// Sign the transaction and get the bytes, which we pass to eth_sendRawTransaction
		jsonRPCMethod = "eth_sendRawTransaction"
		signed, err := tx.Signer.Sign(tx.EthTX)
		if err != nil {
			return "", err
		}
		callParam0 = common.ToHex(signed)
	}

	var txHash string
	err := rpc.CallContext(ctx, &txHash, jsonRPCMethod, callParam0)
	return txHash, err
}
