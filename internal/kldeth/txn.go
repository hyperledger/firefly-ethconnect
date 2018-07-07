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
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"

	"github.com/ethereum/go-ethereum/core/types"
	log "github.com/sirupsen/logrus"
)

// Txn wraps an ethereum transaction, along with the logic to send it over
// JSON/RPC to a node
type Txn struct {
	From    common.Address
	EthTX   *types.Transaction
	Hash    string
	Receipt TxnReceipt
}

// TxnReceipt is the receipt obtained over JSON/RPC from the ethereum client
type TxnReceipt struct {
	BlockHash         *common.Hash    `json:"blockHash"`
	BlockNumber       *hexutil.Big    `json:"blockNumber"`
	ContractAddress   *common.Address `json:"contractAddress"`
	CumulativeGasUsed *hexutil.Big    `json:"cumulativeGasUsed"`
	TransactionHash   *common.Hash    `json:"transactionHash"`
	From              *common.Address `json:"from"`
	GasUsed           *hexutil.Big    `json:"gasUsed"`
	Status            *hexutil.Big    `json:"status"`
	To                *common.Address `json:"to"`
	TransactionIndex  *hexutil.Uint   `json:"transactionIndex"`
}

// NewContractDeployTxn builds a new ethereum transaction from the supplied
// SendTranasction message
func NewContractDeployTxn(msg *kldmessages.DeployContract) (pTX *Txn, err error) {

	var tx Txn
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

// NewSendTxn builds a new ethereum transaction from the supplied
// SendTranasction message
func NewSendTxn(msg *kldmessages.SendTransaction) (pTX *Txn, err error) {

	var tx Txn
	pTX = &tx

	if msg.Method.Name == "" {
		err = fmt.Errorf("Method name must be supplied in 'method.name'")
		return
	}
	methodABI, err := genMethodABI(&msg.Method)
	if err != nil {
		return
	}

	// Build correctly typed args for the ethereum call
	typedArgs, err := pTX.generateTypedArgs(msg.Parameters, *methodABI)
	if err != nil {
		return
	}

	// Pack the arguments
	packedArgs, err := methodABI.Inputs.Pack(typedArgs...)
	if err != nil {
		err = fmt.Errorf("Packing arguments for method '%s': %s", methodABI.Name, err)
		return
	}
	methodID := methodABI.Id()
	log.Infof("Method Name=%s ID=%x PackedArgs=%x", msg.Method.Name, methodID, packedArgs)
	packedCall := append(methodID, packedArgs...)

	// Generate the ethereum transaction
	err = pTX.genEthTransaction(msg.From, msg.To, msg.Nonce, msg.Value, msg.Gas, msg.GasPrice, packedCall)
	return
}

func genMethodABI(jsonABI *kldmessages.ABIMethod) (method *abi.Method, err error) {
	method = &abi.Method{}
	method.Name = jsonABI.Name
	for i := 0; i < len(jsonABI.Inputs); i++ {
		jsonInput := jsonABI.Inputs[i]
		var arg abi.Argument
		arg.Name = jsonInput.Name
		if arg.Type, err = abi.NewType(jsonInput.Type); err != nil {
			err = fmt.Errorf("ABI input %d: Unable to map %s to etherueum type: %s", i, jsonInput.Name, err)
			return
		}
		method.Inputs = append(method.Inputs, arg)
	}
	for i := 0; i < len(jsonABI.Outputs); i++ {
		jsonOutput := jsonABI.Outputs[i]
		var arg abi.Argument
		arg.Name = jsonOutput.Name
		if arg.Type, err = abi.NewType(jsonOutput.Type); err != nil {
			err = fmt.Errorf("ABI output %d: Unable to map %s to etherueum type: %s", i, jsonOutput.Name, err)
			return
		}
		method.Outputs = append(method.Outputs, arg)
	}
	return
}

func (tx *Txn) genEthTransaction(msgFrom, msgTo string, msgNonce, msgValue, msgGas, msgGasPrice json.Number, data []byte) (err error) {

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
	if msgValue.String() != "" {
		if _, ok := value.SetString(msgValue.String(), 10); !ok {
			err = fmt.Errorf("Converting supplied 'value' to big integer: %s", err)
			return
		}
	}

	gas, err := msgGas.Int64()
	if err != nil {
		err = fmt.Errorf("Converting supplied 'gas' to integer: %s", err)
		return
	}

	gasPrice := big.NewInt(0)
	if msgGasPrice.String() != "" {
		if _, ok := value.SetString(msgGasPrice.String(), 10); !ok {
			err = fmt.Errorf("Converting supplied 'gasPrice' to big integer")
			return
		}
	}

	var toAddr common.Address
	var toStr string
	if msgTo != "" {
		if toAddr, err = kldutils.StrToAddress("to", msgTo); err != nil {
			return
		}
		tx.EthTX = types.NewTransaction(uint64(nonce), toAddr, value, uint64(gas), gasPrice, data)
		toStr = toAddr.Hex()
	} else {
		tx.EthTX = types.NewContractCreation(uint64(nonce), value, uint64(gas), gasPrice, data)
		toStr = ""
	}
	etx := tx.EthTX
	log.Debugf("TX:%s From='%s' To='%s' Nonce=%d Value=%d Gas=%d GasPrice=%d",
		etx.Hash().Hex(), tx.From.Hex(), toStr, etx.Nonce(), etx.Value(), etx.Gas(), etx.GasPrice())
	return
}

func getInteger(methodName string, idx int, requiredType string, suppliedType reflect.Type, param interface{}) (val int64, err error) {
	if suppliedType.Kind() == reflect.String {
		if val, err = strconv.ParseInt(param.(string), 10, 64); err != nil {
			err = fmt.Errorf("Method '%s' param %d: Could not be converted to a number", methodName, idx)
			return
		}
	} else if suppliedType.Kind() == reflect.Float64 {
		val = int64(param.(float64))
	} else {
		err = fmt.Errorf("Method '%s' param %d is a %s: Must supply a number or a string", methodName, idx, requiredType)
	}
	return
}

func processIntArray(typedArgs []interface{}, methodName string, idx int, requiredType string, suppliedType reflect.Type, param interface{}) (updatedArgs []interface{}, err error) {
	updatedArgs = typedArgs
	if suppliedType.Kind() == reflect.Slice {
		requiredBaseType := strings.SplitN(requiredType, "[", 2)[0]
		paramV := reflect.ValueOf(param)
		genericParams := make([]interface{}, paramV.Len())
		paramSlice := []interface{}{}
		if paramV.Len() > 0 {
			for i := 0; i < paramV.Len(); i++ {
				genericParams[i] = paramV.Index(i).Interface()
			}
			for _, paramInSlice := range genericParams {
				// Process the entries as integers
				suppliedItemType := reflect.TypeOf(paramInSlice)
				if paramSlice, err = processIntVal(paramSlice, methodName, idx, requiredBaseType, suppliedItemType, paramInSlice); err != nil {
					return
				}
			}
		} else {
			// We don't have an input value, so see what we get supplying a float for the type
			if paramSlice, err = processIntVal(paramSlice, methodName, idx, requiredBaseType, reflect.TypeOf(float64(1)), float64(1)); err != nil {
				return
			}
		}
		targetType := reflect.TypeOf(paramSlice[0])
		targetSliceType := reflect.SliceOf(targetType)
		targetSlice := reflect.MakeSlice(targetSliceType, paramV.Len(), paramV.Len())
		for i := 0; i < paramV.Len(); i++ {
			targetSlice.Index(i).Set(reflect.ValueOf(paramSlice[i]))
		}
		updatedArgs = append(typedArgs, targetSlice.Interface())

	} else {
		err = fmt.Errorf("Method '%s' param %d is a %s: Must supply an array", methodName, idx, requiredType)
	}
	return
}

func processIntVal(typedArgs []interface{}, methodName string, idx int, requiredType string, suppliedType reflect.Type, param interface{}) (updatedArgs []interface{}, err error) {
	var intVal int64
	updatedArgs = typedArgs
	if requiredType == "uint8" {
		if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
			updatedArgs = append(typedArgs, uint8(intVal))
		}
	} else if requiredType == "uint16" {
		if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
			updatedArgs = append(typedArgs, uint16(intVal))
		}
	} else if requiredType == "uint32" {
		if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
			updatedArgs = append(typedArgs, uint32(intVal))
		}
	} else if requiredType == "uint64" {
		if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
			updatedArgs = append(typedArgs, uint64(intVal))
		}
	} else if requiredType == "int8" {
		if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
			updatedArgs = append(typedArgs, int8(intVal))
		}
	} else if requiredType == "int16" {
		if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
			updatedArgs = append(typedArgs, int16(intVal))
		}
	} else if requiredType == "int32" {
		if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
			updatedArgs = append(typedArgs, int32(intVal))
		}
	} else if requiredType == "int64" {
		if intVal, err = getInteger(methodName, idx, requiredType, suppliedType, param); err == nil {
			updatedArgs = append(typedArgs, int64(intVal))
		}
	} else if strings.HasPrefix(requiredType, "int") || strings.HasPrefix(requiredType, "uint") {
		if suppliedType.Kind() == reflect.String {
			bigInt := big.NewInt(0)
			if _, ok := bigInt.SetString(param.(string), 10); !ok {
				err = fmt.Errorf("Method '%s' param %d: Could not be converted to a number", methodName, idx)
			} else {
				updatedArgs = append(typedArgs, bigInt)
			}
		} else if suppliedType.Kind() == reflect.Float64 {
			updatedArgs = append(typedArgs, big.NewInt(int64(param.(float64))))
		} else {
			err = fmt.Errorf("Method '%s' param %d is a %s: Must supply a number or a string", methodName, idx, requiredType)
		}
	} else {
		err = fmt.Errorf("Type '%s' is not yet supported", requiredType)
	}
	if err != nil {
		log.Errorf("processIntVal: %s [Required=%s Supplied=%s Value=%s]", err, requiredType, suppliedType, param)
	}
	return
}

// GenerateTypedArgs parses string arguments into a range of types to pass to the ABI call
func (tx *Txn) generateTypedArgs(params []interface{}, method abi.Method) (typedArgs []interface{}, err error) {

	methodName := method.Name
	if methodName == "" {
		methodName = "<constructor>"
	}
	log.Debug("Parsing args for function: ", method)
	for idx, inputArg := range method.Inputs {
		if idx >= len(params) {
			err = fmt.Errorf("Method '%s': Requires %d args (supplied=%d)", methodName, len(method.Inputs), len(params))
			return
		}
		param := params[idx]
		requiredType := inputArg.Type.String()
		suppliedType := reflect.TypeOf(param)
		if requiredType == "string" {
			if suppliedType.Kind() == reflect.String {
				typedArgs = append(typedArgs, param.(string))
			} else {
				err = fmt.Errorf("Method '%s' param %d: Must supply a string", methodName, idx)
				break
			}
		} else if strings.Contains(requiredType, "int") && strings.HasSuffix(requiredType, "]") {
			typedArgs, err = processIntArray(typedArgs, methodName, idx, requiredType, suppliedType, param)
		} else if strings.Contains(requiredType, "int") {
			typedArgs, err = processIntVal(typedArgs, methodName, idx, requiredType, suppliedType, param)
		} else if requiredType == "bool" {
			if suppliedType.Kind() == reflect.String {
				typedArgs = append(typedArgs, strings.ToLower(param.(string)) == "true")
			} else if suppliedType.Kind() == reflect.Bool {
				typedArgs = append(typedArgs, param.(bool))
			} else {
				err = fmt.Errorf("Method '%s' param %d is a %s: Must supply a boolean or a string", methodName, idx, requiredType)
			}
		} else if requiredType == "address" {
			if suppliedType.Kind() == reflect.String {
				if !common.IsHexAddress(param.(string)) {
					err = fmt.Errorf("Method '%s' param %d: Could not be converted to a hex address", methodName, idx)
				} else {
					typedArgs = append(typedArgs, common.HexToAddress(param.(string)))
				}
			} else {
				err = fmt.Errorf("Method '%s' param %d is a %s: Must supply a hex address string", methodName, idx, requiredType)
			}
		} else if strings.HasPrefix(requiredType, "bytes") {
			if suppliedType.Kind() == reflect.String {
				bSlice := common.FromHex(param.(string))
				if len(bSlice) == 0 {
					typedArgs = append(typedArgs, [0]byte{})
				} else {
					// Create ourselves an array of the right size (ethereum won't accept a slice)
					bArrayType := reflect.ArrayOf(len(bSlice), reflect.TypeOf(bSlice[0]))
					bNewArray := reflect.New(bArrayType).Elem()
					reflect.Copy(bNewArray, reflect.ValueOf(bSlice))
					typedArgs = append(typedArgs, bNewArray.Interface())
				}
			} else {
				err = fmt.Errorf("Method '%s' param %d is a %s: Must supply a hex string", methodName, idx, requiredType)
			}
		} else {
			err = fmt.Errorf("Type '%s' is not yet supported", requiredType)
		}
		if err != nil {
			log.Errorf("%s [Required=%s Supplied=%s Value=%s]", err, requiredType, suppliedType, param)
			return
		}
	}

	return
}
