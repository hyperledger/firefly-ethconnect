// Copyright 2018, 2021 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package eth

import (
	"context"
	"encoding/hex"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"

	"github.com/hyperledger/firefly-ethconnect/internal/ethbind"
	log "github.com/sirupsen/logrus"
)

const (
	errorFunctionSelector = "0x08c379a0" // per https://solidity.readthedocs.io/en/v0.4.24/control-structures.html the signature of Error(string)
)

// calculateGas uses eth_estimateGas to estimate the gas required, providing a buffer
// of 20% for variation as the chain changes between estimation and submission.
func (tx *Txn) calculateGas(ctx context.Context, rpc RPCClient, txArgs *SendTXArgs, gas *ethbinding.HexUint64) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := rpc.CallContext(ctx, &gas, "eth_estimateGas", txArgs); err != nil {
		// Now we attempt a call of the transaction, because that will return us a useful error in the case, of a revert.
		estError := errors.Errorf(errors.TransactionSendGasEstimateFailed, err)
		log.Errorf(estError.Error())
		if _, err := tx.Call(ctx, rpc, "latest"); err != nil {
			return err
		}
		// If the call succeeds, after estimate completed - we still need to fail with the estimate error
		return estError
	}
	*gas = ethbinding.HexUint64(float64(*gas) * 1.2)
	return nil
}

// Call synchronously calls the method, without mining a transaction, and returns the result as RLP encoded bytes or nil
func (tx *Txn) Call(ctx context.Context, rpc RPCClient, blocknumber string) (res []byte, err error) {
	data := ethbinding.HexBytes(tx.EthTX.Data())
	txArgs := &SendTXArgs{
		From:     tx.From.Hex(),
		GasPrice: ethbinding.HexBigInt(*tx.EthTX.GasPrice()),
		Value:    ethbinding.HexBigInt(*tx.EthTX.Value()),
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
		return nil, errors.Errorf(errors.TransactionSendCallFailedNoRevert, err)
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
			return nil, errors.Errorf(errors.TransactionSendCallFailedRevertNoMessage)
		}
		return nil, errors.Errorf(errors.TransactionSendCallFailedRevertMessage, errorStringBytes)
	}
	log.Debugf("eth_call response: %s", hexString)
	res = ethbind.API.FromHex(hexString)
	return
}

func (tx *Txn) CallAndProcessReply(ctx context.Context, rpc RPCClient, blocknumber string) (map[string]interface{}, error) {
	callOption := "latest"
	// only allowed values are "earliest/latest/pending", "", a number string "12345" or a hex number "0xab23"
	// "latest" and "" (no fly-blocknumber given) are equivalent
	if blocknumber != "" && blocknumber != "latest" {
		isHex, _ := regexp.MatchString(`^0x[0-9a-fA-F]+$`, blocknumber)
		if isHex || blocknumber == "earliest" || blocknumber == "pending" {
			callOption = blocknumber
		} else {
			n := new(big.Int)
			n, ok := n.SetString(blocknumber, 10)
			if !ok {
				return nil, errors.Errorf(errors.TransactionCallInvalidBlockNumber)
			}
			callOption = ethbind.API.EncodeBig(n)
		}
	}

	retBytes, err := tx.Call(ctx, rpc, callOption)
	if err != nil || retBytes == nil {
		return nil, err
	}
	return ProcessRLPBytes(tx.Method.Outputs, retBytes), nil
}

// Send sends an individual transaction, choosing external or internal signing
func (tx *Txn) Send(ctx context.Context, rpc RPCClient) (err error) {
	start := time.Now().UTC()

	gas := ethbinding.HexUint64(tx.EthTX.Gas())
	data := ethbinding.HexBytes(tx.EthTX.Data())
	txArgs := &SendTXArgs{
		From:     tx.From.Hex(),
		GasPrice: ethbinding.HexBigInt(*tx.EthTX.GasPrice()),
		Value:    ethbinding.HexBigInt(*tx.EthTX.Value()),
		Data:     &data,
	}
	var to = tx.EthTX.To()
	if to != nil {
		txArgs.To = to.Hex()
	}
	if uint64(gas) == uint64(0) {
		if err = tx.calculateGas(ctx, rpc, txArgs, &gas); err != nil {
			return err
		}
		// Re-encode the EthTX (for external HD Wallet signing)
		if to != nil {
			tx.EthTX = ethbind.API.NewTransaction(tx.EthTX.Nonce(), *tx.EthTX.To(), tx.EthTX.Value(), uint64(gas), tx.EthTX.GasPrice(), tx.EthTX.Data())
		} else {
			tx.EthTX = ethbind.API.NewContractCreation(tx.EthTX.Nonce(), tx.EthTX.Value(), uint64(gas), tx.EthTX.GasPrice(), tx.EthTX.Data())
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
	Nonce    *ethbinding.HexUint64 `json:"nonce,omitempty"`
	From     string                `json:"from"`
	To       string                `json:"to,omitempty"`
	Gas      *ethbinding.HexUint64 `json:"gas,omitempty"`
	GasPrice ethbinding.HexBigInt  `json:"gasPrice,omitempty"`
	Value    ethbinding.HexBigInt  `json:"value,omitempty"`
	Data     *ethbinding.HexBytes  `json:"data"`
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
	var nonce *ethbinding.HexUint64
	if !tx.NodeAssignNonce {
		hexNonce := ethbinding.HexUint64(tx.EthTX.Nonce())
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
			return "", errors.Errorf(errors.TransactionSendMissingPrivateFromOrion)
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
			return "", errors.Errorf(errors.TransactionSendPrivateTXWithExternalSigner, tx.Signer.Type())
		}
		// Sign the transaction and get the bytes, which we pass to eth_sendRawTransaction
		jsonRPCMethod = "eth_sendRawTransaction"
		signed, err := tx.Signer.Sign(tx.EthTX)
		if err != nil {
			return "", err
		}
		callParam0 = ethbind.API.HexEncode(signed)
	}

	var txHash string
	err := rpc.CallContext(ctx, &txHash, jsonRPCMethod, callParam0)
	return txHash, err
}
