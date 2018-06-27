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
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
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

	var tx KldTx
	pTX = &tx

	// Compile the solidity contract
	compiledSolidity, err := CompileContract(msg.Solidity, msg.ContractName)
	if err != nil {
		return
	}

	// Build correctly typed args for the ethereum call
	typedArgs, err := pTX.generateTypedArgs(msg.Parameters, compiledSolidity.ABI.Constructor)
	if err != nil {
		return
	}

	// Pack the arguments
	packedCall, err := compiledSolidity.ABI.Pack("", typedArgs...)
	if err != nil {
		err = fmt.Errorf("Packing arguments for constructor: %s", err)
		return
	}

	// Join the EVM bytecode with the packed call
	data := append(compiledSolidity.Compiled, packedCall...)

	// Generate the ethereum transaction
	err = pTX.genEthTransaction(msg.From, "", msg.Nonce, msg.Value, msg.Gas, msg.GasPrice, data)
	return
}

// Send sends an individual transaction, choosing external or internal signing
func (tx *KldTx) Send(rpc rpcClient) (string, error) {
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
func (tx *KldTx) sendUnsignedTxn(ctx context.Context, rpc rpcClient) (string, error) {
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

func (tx *KldTx) genEthTransaction(msgFrom, msgTo string, msgNonce, msgValue, msgGas, msgGasPrice json.Number, data []byte) (err error) {

	tx.From, err = kldutils.StrToAddress("from", msgFrom)
	if err != nil {
		return
	}

	nonce, err := msgNonce.Int64()
	if err != nil {
		err = fmt.Errorf("Converting supplied 'nonce' to integer: %s", err)
		return
	}

	value := big.NewInt(0)
	if _, ok := value.SetString(msgValue.String(), 10); !ok {
		err = fmt.Errorf("Converting supplied 'value' to big integer: %s", err)
	}

	gas, err := msgGas.Int64()
	if err != nil {
		err = fmt.Errorf("Converting supplied 'gas' to integer: %s", err)
		return
	}

	gasPrice := big.NewInt(0)
	if _, ok := value.SetString(msgGasPrice.String(), 10); !ok {
		err = fmt.Errorf("Converting supplied 'gasPrice' to big integer")
	}

	var toAddr common.Address
	if msgTo != "" {
		if toAddr, err = kldutils.StrToAddress("to", msgTo); err != nil {
			return
		}
		tx.EthTX = types.NewTransaction(uint64(nonce), toAddr, value, uint64(gas), gasPrice, data)
	} else {
		tx.EthTX = types.NewContractCreation(uint64(nonce), value, uint64(gas), gasPrice, data)
	}
	log.Debug("TX:%s To=%s Value=%d Gas=%d GasPrice=%d",
		tx.EthTX.Hash().Hex(), tx.EthTX.To(), tx.EthTX.Value, tx.EthTX.Gas, tx.EthTX.GasPrice)
	return
}

func getInteger(methodName string, idx int, requiredType string, suppliedType reflect.Type, param interface{}) (val int64, err error) {
	if suppliedType.Kind() == reflect.String {
		if val, err = strconv.ParseInt(param.(string), 10, 64); err != nil {
			err = fmt.Errorf("Function '%s' param %d: Could not be converted to a number", methodName, idx)
			return
		}
	} else if suppliedType.Kind() == reflect.Float64 {
		val = int64(param.(float64))
	} else {
		err = fmt.Errorf("Function '%s' param %d is a %s: Must supply a number or a string", methodName, idx, requiredType)
	}
	return
}

// GenerateTypedArgs parses string arguments into a range of types to pass to the ABI call
func (tx *KldTx) generateTypedArgs(params []interface{}, method abi.Method) (typedArgs []interface{}, err error) {

	methodName := method.Name
	if methodName == "" {
		methodName = "<constructor>"
	}
	log.Debug("Parsing args for function: ", method)
	var intVal int64
	for idx, inputArg := range method.Inputs {
		if idx >= len(params) {
			err = fmt.Errorf("Function '%s': Requires %d args (supplied=%d)", methodName, len(method.Inputs), len(params))
			return
		}
		param := params[idx]
		requiredType := inputArg.Type.String()
		suppliedType := reflect.TypeOf(param)
		if requiredType == "string" {
			if suppliedType.Kind() == reflect.String {
				typedArgs = append(typedArgs, param.(string))
			} else {
				err = fmt.Errorf("Function '%s' param %d: Must supply a string", methodName, idx)
				break
			}
		} else if requiredType == "uint8" {
			if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
				typedArgs = append(typedArgs, uint8(intVal))
			}
		} else if requiredType == "uint16" {
			if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
				typedArgs = append(typedArgs, uint16(intVal))
			}
		} else if requiredType == "uint32" {
			if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
				typedArgs = append(typedArgs, uint32(intVal))
			}
		} else if requiredType == "uint64" {
			if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
				typedArgs = append(typedArgs, uint64(intVal))
			}
		} else if requiredType == "int8" {
			if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
				typedArgs = append(typedArgs, int8(intVal))
			}
		} else if requiredType == "int16" {
			if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
				typedArgs = append(typedArgs, int16(intVal))
			}
		} else if requiredType == "int32" {
			if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
				typedArgs = append(typedArgs, int32(intVal))
			}
		} else if requiredType == "int64" {
			if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
				typedArgs = append(typedArgs, int64(intVal))
			}
		} else if strings.HasPrefix(requiredType, "int") || strings.HasPrefix(requiredType, "uint") {
			if suppliedType.Kind() == reflect.String {
				bigInt := big.NewInt(0)
				if _, ok := bigInt.SetString(param.(string), 10); !ok {
					err = fmt.Errorf("Function '%s' param %d: Could not be converted to a number", methodName, idx)
				} else {
					typedArgs = append(typedArgs, bigInt)
				}
			} else if suppliedType.Kind() == reflect.Float64 {
				typedArgs = append(typedArgs, big.NewInt(int64(param.(float64))))
			} else {
				err = fmt.Errorf("Function '%s' param %d is a %s: Must supply a number or a string", methodName, idx, requiredType)
			}
		} else if requiredType == "bool" {
			if suppliedType.Kind() == reflect.String {
				typedArgs = append(typedArgs, strings.ToLower(param.(string)) == "true")
			} else if suppliedType.Kind() == reflect.Bool {
				typedArgs = append(typedArgs, param.(bool))
			} else {
				err = fmt.Errorf("Function '%s' param %d is a %s: Must supply a boolean or a string", methodName, idx, requiredType)
			}
		} else if requiredType == "address" {
			if suppliedType.Kind() == reflect.String {
				if !common.IsHexAddress(param.(string)) {
					err = fmt.Errorf("Function '%s' param %d: Could not be converted to a hex address", methodName, idx)
				} else {
					typedArgs = append(typedArgs, common.HexToAddress(param.(string)))
				}
			} else {
				err = fmt.Errorf("Function '%s' param %d is a %s: Must supply a hex address string", methodName, idx, requiredType)
			}
		} else if strings.HasPrefix(requiredType, "bytes") {
			if suppliedType.Kind() == reflect.String {
				bSlice := common.FromHex(param.(string))
				if len(bSlice) == 0 {
					typedArgs = append(typedArgs, [0]byte{})
				} else {
					// Create ourselves an array of the right size (ethereum won't accept a slice)
					bArrayType := reflect.ArrayOf(len(bSlice), reflect.TypeOf(bSlice[0]))
					bArrayVal := reflect.Zero(bArrayType)
					reflect.Copy(reflect.ValueOf(bSlice), bArrayVal)
					typedArgs = append(typedArgs, bArrayVal.Interface())
				}
			} else {
				err = fmt.Errorf("Function '%s' param %d is a %s: Must supply a hex string", methodName, idx, requiredType)
			}
		} else {
			err = fmt.Errorf("Type '%s' is not yet supported", inputArg.Type)
		}
		if err != nil {
			log.Errorf("%s [Required=%s Supplied=%s Value=%s]", err, requiredType, suppliedType, param)
			return
		}
	}

	return
}
