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
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"

	"github.com/ethereum/go-ethereum/core/types"
	log "github.com/sirupsen/logrus"
)

// Txn wraps an ethereum transaction, along with the logic to send it over
// JSON/RPC to a node
type Txn struct {
	NodeAssignNonce  bool
	OrionPrivateAPIS bool
	From             common.Address
	EthTX            *types.Transaction
	Hash             string
	Receipt          TxnReceipt
	PrivateFrom      string
	PrivateFor       []string
	PrivacyGroupID   string
	Signer           TXSigner
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
func NewContractDeployTxn(msg *kldmessages.DeployContract, signer TXSigner) (tx *Txn, err error) {

	tx = &Txn{Signer: signer}

	var compiled *CompiledSolidity

	if msg.Compiled != nil && msg.ABI != nil {
		compiled = &CompiledSolidity{
			Compiled: msg.Compiled,
			ABI:      msg.ABI,
		}
	} else if msg.Solidity != "" {
		// Compile the solidity contract
		if compiled, err = CompileContract(msg.Solidity, msg.ContractName, msg.CompilerVersion, msg.EVMVersion); err != nil {
			return
		}
	} else {
		err = klderrors.Errorf(klderrors.DeployTransactionMissingCode)
		return
	}

	// Build a runtime ABI from the serialized one
	var typedArgs []interface{}
	abi, err := kldbind.ABIMarshalingToABIRuntime(compiled.ABI)
	if err == nil {
		// Build correctly typed args for the ethereum call
		typedArgs, err = tx.generateTypedArgs(msg.Parameters, &abi.Constructor)
	}
	if err != nil {
		return
	}

	// Pack the arguments
	packedCall, err := abi.Pack("", typedArgs...)
	if err != nil {
		err = klderrors.Errorf(klderrors.TransactionSendConstructorPackArgs, err)
		return
	}

	// Join the EVM bytecode with the packed call
	data := append(compiled.Compiled, packedCall...)

	from := msg.From
	if tx.Signer != nil {
		from = tx.Signer.Address()
	}

	// Generate the ethereum transaction
	if err = tx.genEthTransaction(from, "", msg.Nonce, msg.Value, msg.Gas, msg.GasPrice, data); err != nil {
		return
	}

	// retain private transaction fields
	tx.PrivateFrom = msg.PrivateFrom
	tx.PrivateFor = msg.PrivateFor
	tx.PrivacyGroupID = msg.PrivacyGroupID
	return
}

// CallMethod performs eth_call to return data from the chain
func CallMethod(ctx context.Context, rpc RPCClient, signer TXSigner, from, addr string, value json.Number, methodABI *kldbind.ABIMethod, msgParams []interface{}, blocknumber string) (map[string]interface{}, error) {
	log.Debugf("Calling method. ABI: %+v Params: %+v", methodABI, msgParams)
	tx, err := buildTX(signer, from, addr, "", value, "", "", methodABI, msgParams)
	if err != nil {
		return nil, err
	}
	callOption := "latest"
	// only allowed values are "earliest/latest/pending", "", a number string "12345" or a hex number "0xab23"
	// "latest" and "" (no kld-blocknumber given) are equivalent
	if blocknumber != "" && blocknumber != "latest" {
		isHex, _ := regexp.MatchString(`^0x[0-9a-fA-F]+$`, blocknumber)
		if isHex || blocknumber == "earliest" || blocknumber == "pending" {
			callOption = blocknumber
		} else {
			n := new(big.Int)
			n, ok := n.SetString(blocknumber, 10)
			if !ok {
				return nil, klderrors.Errorf(klderrors.TransactionCallInvalidBlockNumber)
			}
			callOption = hexutil.EncodeBig(n)
		}
	}

	retBytes, err := tx.Call(ctx, rpc, callOption)
	if err != nil || retBytes == nil {
		return nil, err
	}
	return ProcessRLPBytes(methodABI.Outputs, retBytes)
}

// ProcessRLPBytes converts binary packed set of bytes into a map
func ProcessRLPBytes(args abi.Arguments, retBytes []byte) (map[string]interface{}, error) {
	retval := make(map[string]interface{})
	rawRetval, unpackErr := args.UnpackValues(retBytes)
	var err error
	if unpackErr != nil {
		err = klderrors.Errorf(klderrors.UnpackOutputsFailed, unpackErr)
	} else {
		err = processOutputs(args, rawRetval, retval)
	}
	if err != nil {
		log.Errorf("Unable to process bytes '%s': %s", rawRetval, err)
	}
	return retval, err
}

func processOutputs(args abi.Arguments, rawRetval []interface{}, retval map[string]interface{}) error {
	numOutputs := len(args)
	if numOutputs > 0 {
		if len(rawRetval) != numOutputs {
			return klderrors.Errorf(klderrors.UnpackOutputsMismatchCount, numOutputs, len(rawRetval), rawRetval)
		}
		for idx, output := range args {
			if err := genOutput(idx, retval, output, rawRetval[idx]); err != nil {
				return err
			}
		}
	} else if rawRetval != nil {
		return klderrors.Errorf(klderrors.UnpackOutputsMismatchNil, rawRetval)
	}
	return nil
}

func genOutput(idx int, retval map[string]interface{}, output abi.Argument, rawValue interface{}) (err error) {
	// Match the swagger in how we name the outputs
	argName := output.Name
	if argName == "" {
		argName = "output"
		if idx != 0 {
			argName += strconv.Itoa(idx)
		}
	}
	retval[argName], err = mapOutput(argName, output.Type.String(), &output.Type, rawValue)
	return
}

func mapOutput(argName, argType string, t *abi.Type, rawValue interface{}) (interface{}, error) {
	rawType := reflect.TypeOf(rawValue)
	switch t.T {
	case abi.IntTy, abi.UintTy:
		kind := rawType.Kind()
		if kind == reflect.Ptr && rawType.String() == "*big.Int" {
			return reflect.ValueOf(rawValue).Interface().(*big.Int).String(), nil
		} else if kind == reflect.Int ||
			kind == reflect.Int8 ||
			kind == reflect.Int16 ||
			kind == reflect.Int32 ||
			kind == reflect.Int64 {
			return strconv.FormatInt(reflect.ValueOf(rawValue).Int(), 10), nil
		} else if kind == reflect.Uint ||
			kind == reflect.Uint8 ||
			kind == reflect.Uint16 ||
			kind == reflect.Uint32 ||
			kind == reflect.Uint64 {
			return strconv.FormatUint(reflect.ValueOf(rawValue).Uint(), 10), nil
		} else {
			return nil, klderrors.Errorf(klderrors.UnpackOutputsMismatchType, "number",
				argName, argType, rawType.Kind())
		}
	case abi.BoolTy:
		if rawType.Kind() != reflect.Bool {
			return nil, klderrors.Errorf(klderrors.UnpackOutputsMismatchType, "boolean",
				argName, argType, rawType.Kind())
		}
		return rawValue, nil
	case abi.StringTy:
		if rawType.Kind() != reflect.String {
			return nil, klderrors.Errorf(klderrors.UnpackOutputsMismatchType, "string array",
				argName, argType, rawType.Kind())
		}
		return reflect.ValueOf(rawValue).Interface().(string), nil
	case abi.BytesTy, abi.FixedBytesTy, abi.AddressTy:
		if (rawType.Kind() != reflect.Array && rawType.Kind() != reflect.Slice) || rawType.Elem().Kind() != reflect.Uint8 {
			return nil, klderrors.Errorf(klderrors.UnpackOutputsMismatchType, "[]byte",
				argName, argType, rawType.Kind())
		}
		s := reflect.ValueOf(rawValue)
		arrayVal := make([]byte, s.Len())
		for i := 0; i < s.Len(); i++ {
			arrayVal[i] = byte(s.Index(i).Uint())
		}
		return common.ToHex(arrayVal), nil
	case abi.SliceTy, abi.ArrayTy:
		if rawType.Kind() != reflect.Slice {
			return nil, klderrors.Errorf(klderrors.UnpackOutputsMismatchType, "slice",
				argName, argType, rawType.Kind())
		}
		s := reflect.ValueOf(rawValue)
		arrayVal := make([]interface{}, 0, s.Len())
		for i := 0; i < s.Len(); i++ {
			mapped, err := mapOutput(fmt.Sprintf("%s[%d]", argName, i), argType, t.Elem, s.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			arrayVal = append(arrayVal, mapped)
		}
		return arrayVal, nil
	case abi.TupleTy:
		return genTupleMapOutput(argName, argType, t, rawValue)
	default:
		return nil, klderrors.Errorf(klderrors.UnpackOutputsUnknownType,
			argName, argType, rawType.Kind())
	}
}

func genTupleMapOutput(argName, argType string, t *abi.Type, rawValue interface{}) (r map[string]interface{}, err error) {
	reflectValue := reflect.ValueOf(rawValue)
	if reflectValue.Kind() != reflect.Struct || reflectValue.Type() != t.TupleType {
		return nil, klderrors.Errorf(klderrors.UnpackOutputsMismatchTupleType,
			argName, argType, t.TupleType, rawValue)
	}
	if len(t.TupleRawNames) != reflectValue.NumField() {
		return nil, klderrors.Errorf(klderrors.UnpackOutputsMismatchTupleFieldCount,
			argName, argType, len(t.TupleRawNames), reflectValue.NumField())
	}
	returnMap := make(map[string]interface{})
	for i, fieldName := range t.TupleRawNames {
		returnMap[fieldName], err = mapOutput(fmt.Sprintf("%s.%s", argName, fieldName), t.TupleElems[i].String(), t.TupleElems[i], reflectValue.Field(i).Interface())
		if err != nil {
			return nil, err
		}
	}
	return returnMap, nil
}

// NewSendTxn builds a new ethereum transaction from the supplied
// SendTranasction message
func NewSendTxn(msg *kldmessages.SendTransaction, signer TXSigner) (tx *Txn, err error) {

	var methodABI *abi.Method
	if msg.Method == nil || msg.Method.Name == "" {
		if msg.MethodName == "" {
			err = klderrors.Errorf(klderrors.TransactionSendMissingMethod)
			return
		}
		var abiInputs abi.Arguments
		jsonABI := &kldmessages.ABIMethod{
			Name:    msg.MethodName,
			Inputs:  []kldbind.ABIArgumentMarshaling{},
			Outputs: []kldbind.ABIArgumentMarshaling{},
		}
		msg.Parameters, err = flattenParams(msg.Parameters, &abiInputs, true)
		if err == nil {
			methodABI, err = genMethodABI(jsonABI, abiInputs)
		}
		if err != nil {
			return
		}
	} else {
		methodABI, err = kldbind.ABIElementMarshalingToABIMethod(msg.Method)
		if err != nil {
			return
		}
	}

	if tx, err = buildTX(signer, msg.From, msg.To, msg.Nonce, msg.Value, msg.Gas, msg.GasPrice, methodABI, msg.Parameters); err != nil {
		return
	}

	// retain private transaction fields
	tx.PrivateFrom = msg.PrivateFrom
	tx.PrivateFor = msg.PrivateFor
	return
}

// NewNilTX returns a transaction without any data from/to the same address
func NewNilTX(from string, nonce int64, signer TXSigner) (tx *Txn, err error) {
	tx = &Txn{Signer: signer}
	if tx.Signer != nil {
		from = signer.Address()
	}
	err = tx.genEthTransaction(
		from, from,
		json.Number(strconv.FormatInt(nonce, 10)),
		json.Number("0"), json.Number("90000"), json.Number("0"),
		[]byte{})
	return
}

func buildTX(signer TXSigner, msgFrom, msgTo string, msgNonce, msgValue, msgGas, msgGasPrice json.Number, methodABI *abi.Method, params []interface{}) (tx *Txn, err error) {
	tx = &Txn{Signer: signer}

	// Build correctly typed args for the ethereum call
	typedArgs, err := tx.generateTypedArgs(params, methodABI)
	if err != nil {
		return
	}

	// Pack the arguments
	packedArgs, err := methodABI.Inputs.Pack(typedArgs...)
	if err != nil {
		err = klderrors.Errorf(klderrors.TransactionSendMethodPackArgs, methodABI.RawName, err)
		log.Errorf("Attempted to pack args %+v: %s", typedArgs, err)
		return
	}
	methodID := methodABI.ID
	log.Debugf("Method Name=%s ID=%x PackedArgs=%x", methodABI.RawName, methodID, packedArgs)
	packedCall := append(methodID, packedArgs...)

	from := msgFrom
	if tx.Signer != nil {
		from = signer.Address()
	}

	// Generate the ethereum transaction
	err = tx.genEthTransaction(from, msgTo, msgNonce, msgValue, msgGas, msgGasPrice, packedCall)
	return
}

func genMethodABI(jsonABI *kldmessages.ABIMethod, predeterminedInputs abi.Arguments) (method *abi.Method, err error) {
	var inputs abi.Arguments
	if predeterminedInputs != nil {
		inputs = predeterminedInputs
	} else {
		inputs = make(abi.Arguments, len(jsonABI.Inputs))
		for i := 0; i < len(jsonABI.Inputs); i++ {
			jsonInput := jsonABI.Inputs[i]
			var arg abi.Argument
			arg.Name = jsonInput.Name
			if arg.Type, err = abi.NewType(jsonInput.Type, "", []abi.ArgumentMarshaling{}); err != nil {
				err = klderrors.Errorf(klderrors.TransactionSendInputTypeUnknown, i, jsonInput.Name, err)
				return
			}
			inputs[i] = arg
		}
	}

	outputs := make(abi.Arguments, len(jsonABI.Outputs))
	for i := 0; i < len(jsonABI.Outputs); i++ {
		jsonOutput := jsonABI.Outputs[i]
		var arg abi.Argument
		arg.Name = jsonOutput.Name
		if arg.Type, err = abi.NewType(jsonOutput.Type, "", []abi.ArgumentMarshaling{}); err != nil {
			err = klderrors.Errorf(klderrors.TransactionSendOutputTypeUnknown, i, jsonOutput.Name, err)
			return
		}
		outputs[i] = arg
	}

	var abiMethod abi.Method
	// Note we only support "normal" functions here - constructors do not follow this code path, and we don't have support
	// of Fallback or Receive yet
	switch jsonABI.Type {
	case "function":
	case "":
		abiMethod = abi.NewMethod(jsonABI.Name, jsonABI.Name, abi.Function, jsonABI.StateMutability, jsonABI.Constant, jsonABI.Payable, inputs, outputs)
	default:
		return nil, klderrors.Errorf(klderrors.RESTGatewayMethodTypeInvalid, jsonABI.Type)
	}
	return &abiMethod, nil
}

func (tx *Txn) genEthTransaction(msgFrom, msgTo string, msgNonce, msgValue, msgGas, msgGasPrice json.Number, data []byte) (err error) {

	if msgFrom != "" {
		tx.From, err = kldutils.StrToAddress("from", msgFrom)
		if err != nil {
			return
		}
	}

	var nonce int64
	if msgNonce != "" {
		nonce, err = msgNonce.Int64()
		if err != nil {
			err = klderrors.Errorf(klderrors.TransactionSendBadNonce, err)
			return
		}
	}

	value := big.NewInt(0)
	if msgValue.String() != "" {
		if _, ok := value.SetString(msgValue.String(), 10); !ok {
			err = klderrors.Errorf(klderrors.TransactionSendBadValue, err)
			return
		}
	}

	var gas int64
	if msgGas != "" {
		gas, err = msgGas.Int64()
		if err != nil {
			err = klderrors.Errorf(klderrors.TransactionSendBadGas, err)
			return
		}
	}

	gasPrice := big.NewInt(0)
	if msgGasPrice.String() != "" {
		if _, ok := gasPrice.SetString(msgGasPrice.String(), 10); !ok {
			err = klderrors.Errorf(klderrors.TransactionSendBadGasPrice)
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

func (tx *Txn) getInteger(methodName string, path string, requiredType *abi.Type, suppliedType reflect.Type, param interface{}) (val int64, err error) {
	if suppliedType.Kind() == reflect.String {
		if val, err = strconv.ParseInt(param.(string), 10, 64); err != nil {
			err = klderrors.Errorf(klderrors.TransactionSendInputTypeBadNumber, methodName, path)
			return
		}
	} else if suppliedType.Kind() == reflect.Float64 {
		val = int64(param.(float64))
	} else {
		err = klderrors.Errorf(klderrors.TransactionSendInputTypeBadJSONTypeForNumber, methodName, path, requiredType, suppliedType)
	}
	return
}

func (tx *Txn) getUnsignedInteger(methodName string, path string, requiredType *abi.Type, suppliedType reflect.Type, param interface{}) (val uint64, err error) {
	if suppliedType.Kind() == reflect.String {
		if val, err = strconv.ParseUint(param.(string), 10, 64); err != nil {
			err = klderrors.Errorf(klderrors.TransactionSendInputTypeBadNumber, methodName, path)
			return
		}
	} else if suppliedType.Kind() == reflect.Float64 {
		val = uint64(param.(float64))
	} else {
		err = klderrors.Errorf(klderrors.TransactionSendInputTypeBadJSONTypeForNumber, methodName, path, requiredType, suppliedType)
	}
	return
}

func (tx *Txn) getBigInteger(methodName string, path string, requiredType *abi.Type, suppliedType reflect.Type, param interface{}) (bigInt *big.Int, err error) {
	bigInt = big.NewInt(0)
	if suppliedType.Kind() == reflect.String {
		if _, ok := bigInt.SetString(param.(string), 10); !ok {
			err = klderrors.Errorf(klderrors.TransactionSendInputTypeBadNumber, methodName, path)
		}
	} else if suppliedType.Kind() == reflect.Float64 {
		bigInt.SetInt64(int64(param.(float64)))
	} else {
		err = klderrors.Errorf(klderrors.TransactionSendInputTypeBadJSONTypeForNumber, methodName, path, requiredType, suppliedType)
	}
	return
}

func (tx *Txn) generateTypedArrayOrSlice(methodName string, path string, requiredType *abi.Type, suppliedType reflect.Type, param interface{}) (interface{}, error) {
	if suppliedType.Kind() != reflect.Slice {
		return nil, klderrors.Errorf(klderrors.TransactionSendInputTypeBadJSONTypeForArray, methodName, path, requiredType, suppliedType)
	}
	paramV := reflect.ValueOf(param)
	var genericSlice reflect.Value
	var requiredReflectType = requiredType.GetType()
	if requiredReflectType.Kind() == reflect.Array {
		arrayType := reflect.ArrayOf(requiredType.Size, requiredType.Elem.GetType())
		genericSlice = reflect.New(arrayType).Elem()
	} else {
		genericSlice = reflect.MakeSlice(requiredReflectType, paramV.Len(), paramV.Len())
	}
	innerType := requiredType.Elem
	for i := 0; i < paramV.Len(); i++ {
		paramInSlice := paramV.Index(i).Interface()
		val, err := tx.generateTypedArg(innerType, paramInSlice, methodName, fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		genericSlice.Index(i).Set(reflect.ValueOf(val))
	}
	return genericSlice.Interface(), nil
}

func (tx *Txn) generateTupleFromMap(methodName string, path string, requiredType *abi.Type, param map[string]interface{}) (v interface{}, err error) {
	tuple := reflect.New(requiredType.TupleType).Elem()
	for i, inputElemName := range requiredType.TupleRawNames {
		var typedVal interface{}
		var suppliedType reflect.Type
		inputVal, ok := param[inputElemName]
		if ok {
			typedVal, err = tx.generateTypedArg(requiredType.TupleElems[i], inputVal, methodName, fmt.Sprintf("%s.%s", path, inputElemName))
			if err != nil {
				return nil, err
			}
			suppliedType = reflect.TypeOf(typedVal)
		}
		tupleField := tuple.Field(i)
		if suppliedType == nil {
			// No known cases where nil can be assigned
			return nil, klderrors.Errorf(klderrors.TransactionSendInputNotAssignable, methodName, path, typedVal, inputElemName, requiredType.TupleElems[i])
		}
		if !suppliedType.AssignableTo(tupleField.Type()) {
			return nil, klderrors.Errorf(klderrors.TransactionSendInputNotAssignable, methodName, path, typedVal, inputElemName, requiredType.TupleElems[i])
		}
		tupleField.Set(reflect.ValueOf(typedVal))
	}
	return tuple.Interface(), nil
}

func (tx *Txn) generateTypedArg(requiredType *abi.Type, param interface{}, methodName string, path string) (interface{}, error) {
	suppliedType := reflect.TypeOf(param)
	if suppliedType == nil {
		return nil, klderrors.Errorf(klderrors.TransactionSendInputTypeBadNull, methodName, path)
	}
	switch requiredType.T {
	case abi.IntTy, abi.UintTy:
		if requiredType.Size <= 64 {
			if requiredType.T == abi.IntTy {
				intVal, err := tx.getInteger(methodName, path, requiredType, suppliedType, param)
				if err != nil {
					return nil, err
				}
				switch requiredType.Size {
				case 8:
					return int8(intVal), nil
				case 16:
					return int16(intVal), nil
				case 32:
					return int32(intVal), nil
				case 64:
					return int64(intVal), nil
				}
			} else {
				uintVal, err := tx.getUnsignedInteger(methodName, path, requiredType, suppliedType, param)
				if err != nil {
					return nil, err
				}
				switch requiredType.Size {
				case 8:
					return uint8(uintVal), nil
				case 16:
					return uint16(uintVal), nil
				case 32:
					return uint32(uintVal), nil
				case 64:
					return uint64(uintVal), nil
				}
			}
		}
		// Catch-all is a big.Int - anyting that isn't an exact match power of 2, or greater than 64 bit
		return tx.getBigInteger(methodName, path, requiredType, suppliedType, param)
	case abi.BoolTy:
		if suppliedType.Kind() == reflect.String {
			return (strings.ToLower(param.(string)) == "true"), nil
		} else if suppliedType.Kind() == reflect.Bool {
			return param.(bool), nil
		}
		return nil, klderrors.Errorf(klderrors.TransactionSendInputTypeBadJSONTypeForBoolean, methodName, path, requiredType, suppliedType)
	case abi.StringTy:
		if suppliedType.Kind() == reflect.String {
			return param.(string), nil
		}
		return nil, klderrors.Errorf(klderrors.TransactionSendInputTypeBadJSONTypeForString, methodName, path, suppliedType)
	case abi.AddressTy:
		if suppliedType.Kind() == reflect.String {
			if !common.IsHexAddress(param.(string)) {
				return nil, klderrors.Errorf(klderrors.TransactionSendInputTypeAddress, methodName, path, suppliedType)
			}
			return common.HexToAddress(param.(string)), nil
		}
		return nil, klderrors.Errorf(klderrors.TransactionSendInputTypeBadJSONTypeForAddress, methodName, path, requiredType, suppliedType)
	case abi.BytesTy, abi.FixedBytesTy:
		var bSlice []byte
		if suppliedType.Kind() == reflect.Slice {
			paramV := reflect.ValueOf(param)
			bSliceLen := paramV.Len()
			bSlice = make([]byte, bSliceLen, bSliceLen)
			for i := 0; i < bSliceLen; i++ {
				valV := paramV.Index(i)
				if valV.Kind() == reflect.Interface {
					valV = valV.Elem()
				}
				if valV.Kind() != reflect.Float64 {
					return nil, klderrors.Errorf(klderrors.TransactionSendInputTypeBadJSONTypeInNumericArray, methodName, path, requiredType, i, valV.Kind())
				}
				floatVal := valV.Float()
				if floatVal > 255 || floatVal < 0 {
					return nil, klderrors.Errorf(klderrors.TransactionSendInputTypeBadByteOutsideRange, methodName, path, requiredType)
				}
				bSlice[i] = byte(floatVal)
			}
		} else if suppliedType.Kind() == reflect.String {
			bSlice = common.FromHex(param.(string))
		} else {
			return nil, klderrors.Errorf(klderrors.TransactionSendInputTypeBadJSONTypeForBytes, methodName, path, requiredType, suppliedType)
		}
		if len(bSlice) == 0 {
			return [0]byte{}, nil
		} else if requiredType.GetType().Kind() == reflect.Array {
			// Create ourselves an array of the right size (ethereum won't accept a slice)
			bArrayType := reflect.ArrayOf(len(bSlice), reflect.TypeOf(bSlice[0]))
			bNewArray := reflect.New(bArrayType).Elem()
			reflect.Copy(bNewArray, reflect.ValueOf(bSlice))
			return bNewArray.Interface(), nil
		}
		return bSlice, nil
	case abi.SliceTy, abi.ArrayTy:
		return tx.generateTypedArrayOrSlice(methodName, path, requiredType, suppliedType, param)
	case abi.TupleTy:
		if suppliedType.Kind() != reflect.Map || suppliedType.Key().Kind() != reflect.String {
			return nil, klderrors.Errorf(klderrors.TransactionSendInputTypeBadJSONTypeForTuple, methodName, path, requiredType, suppliedType)
		}
		return tx.generateTupleFromMap(methodName, path, requiredType, param.(map[string]interface{}))
	default:
		return nil, klderrors.Errorf(klderrors.TransactionSendInputTypeNotSupported, requiredType)
	}
}

// GenerateTypedArgs parses string arguments into a range of types to pass to the ABI call
func (tx *Txn) generateTypedArgs(origParams []interface{}, method *abi.Method) ([]interface{}, error) {

	params, err := flattenParams(origParams, &method.Inputs, false)
	if err != nil {
		return nil, err
	}

	methodName := method.Name
	if methodName == "" {
		methodName = "<constructor>"
	}
	log.Debug("Parsing args for function: ", method)
	var typedArgs []interface{}
	for idx, inputArg := range method.Inputs {
		if idx >= len(params) {
			err = klderrors.Errorf(klderrors.TransactionSendInputCountMismatch, methodName, len(method.Inputs), len(params))
			return nil, err
		}
		param := params[idx]
		requiredType := &inputArg.Type
		log.Debugf("Arg %d requiredType: %s", idx, requiredType)
		arg, err := tx.generateTypedArg(requiredType, param, methodName, fmt.Sprintf("%d", idx))
		if err != nil {
			log.Errorf("%s [Required=%s Supplied=%s Value=%+v]", err, requiredType, reflect.TypeOf(param), param)
			return nil, err
		}
		log.Debugf("Arg %d value: %+v (type=%s)", idx, arg, reflect.TypeOf(arg))
		typedArgs = append(typedArgs, arg)
	}
	return typedArgs, nil
}

// flattenParams flattens an array of parameters of the form
// [{"value":"val1","type":"uint256"},{"value":"val2","type":"uint256"}]
// into ["val1","val2"], and updates the abi.Method declaration with any
// types specified.
// If a flat structure is passed in, then there are no changes.
// A mix is tollerated by the code, but no usecase is known for that.
func flattenParams(origParams []interface{}, inputs *abi.Arguments, lazyTyping bool) (params []interface{}, err error) {
	if !lazyTyping && len(origParams) > len(*inputs) {
		err = klderrors.Errorf(klderrors.TransactionSendInputTooManyParams, len(origParams), len(*inputs))
	}
	// Allows us to support
	params = make([]interface{}, len(origParams))
	for i, unflattened := range origParams {
		if unflattened == nil {
			params[i] = nil
		} else if reflect.TypeOf(unflattened).Kind() != reflect.Map {
			// No change needed
			params[i] = unflattened
		} else if len(*inputs) > i && (*inputs)[i].Type.T == abi.TupleTy {
			// No change needed - structure input
			params[i] = unflattened
		} else {
			// We need to flatten
			mapParam := unflattened.(map[string]interface{}) // safe case as we came in from JSON only
			var value, typeStr interface{}
			var exists bool
			if value, exists = mapParam["value"]; exists {
				typeStr, exists = mapParam["type"]
			}
			if !exists {
				err = klderrors.Errorf(klderrors.TransactionSendInputStructureWrong, i)
				return
			}
			if reflect.TypeOf(typeStr).Kind() != reflect.String {
				err = klderrors.Errorf(klderrors.TransactionSendInputInLineTypeArrayNotString, i)
				return
			}
			params[i] = value
			// Set the type
			var ethType abi.Type
			if ethType, err = abi.NewType(typeStr.(string), "", []abi.ArgumentMarshaling{}); err != nil {
				err = klderrors.Errorf(klderrors.TransactionSendInputInLineTypeUnknown, i, typeStr, err)
				return
			}
			for len(*inputs) <= i {
				*inputs = append(*inputs, abi.Argument{})
			}
			(*inputs)[i].Type = ethType
		}
	}
	return
}
