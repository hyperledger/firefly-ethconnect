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
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// Slim interface for stubbing
type testRPCClient struct {
	mockError       error
	capturedMethod  string
	capturedArgs    []interface{}
	capturedMethod2 string
	capturedArgs2   []interface{}
}

func (r *testRPCClient) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	if r.capturedMethod == "" {
		r.capturedMethod = method
		r.capturedArgs = args
	} else {
		r.capturedMethod2 = method
		r.capturedArgs2 = args
	}
	return r.mockError
}

const (
	simpleStorage = "pragma solidity >=0.4.22 <0.6.0;\n\ncontract simplestorage {\nuint public storedData;\n\nconstructor(uint initVal) public {\nstoredData = initVal;\n}\n\nfunction set(uint x) public {\nstoredData = x;\n}\n\nfunction get() public view returns (uint retVal) {\nreturn storedData;\n}\n}"
	twoContracts  = "pragma solidity >=0.4.22 <0.6.0;\n\ncontract contract1 {function f1() public pure returns (uint retVal) {\nreturn 1;\n}\n}\n\ncontract contract2 {function f2() public pure returns (uint retVal) {\nreturn 2;\n}\n}"
)

func TestNewContractDeployTxnSimpleStorage(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{float64(999999)}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	tx, err := NewContractDeployTxn(&msg)
	assert.Nil(err)
	rpc := testRPCClient{}

	tx.Send(&rpc)

	assert.Equal("eth_sendTransaction", rpc.capturedMethod)
	jsonBytesSent, _ := json.Marshal(rpc.capturedArgs[0])
	var jsonSent map[string]interface{}
	json.Unmarshal(jsonBytesSent, &jsonSent)
	assert.Equal("0x7b", jsonSent["nonce"])
	assert.Equal("0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c", jsonSent["from"])
	assert.Equal("0x1c8", jsonSent["gas"])
	assert.Equal("0x0", jsonSent["gasPrice"])
	assert.Equal("0x315", jsonSent["value"])
	// The bytecode has the packed parameters appended to the end
	assert.Regexp(".+00000000000000000000000000000000000000000000000000000000000f423f$", jsonSent["data"])

}

func TestNewContractDeployTxnSimpleStorageCalcGas(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{float64(999999)}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.GasPrice = "789"
	tx, err := NewContractDeployTxn(&msg)
	assert.Nil(err)
	rpc := testRPCClient{}

	tx.Send(&rpc)

	assert.Equal("eth_estimateGas", rpc.capturedMethod)
	assert.Equal("eth_sendTransaction", rpc.capturedMethod2)
	jsonBytesSent, _ := json.Marshal(rpc.capturedArgs[0])
	var jsonSent map[string]interface{}
	json.Unmarshal(jsonBytesSent, &jsonSent)
	assert.Equal("0x7b", jsonSent["nonce"])
	assert.Equal("0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c", jsonSent["from"])
	assert.Equal("0x0", jsonSent["gas"])
	assert.Equal("0x0", jsonSent["gasPrice"])
	assert.Equal("0x315", jsonSent["value"])
	// The bytecode has the packed parameters appended to the end
	assert.Regexp(".+00000000000000000000000000000000000000000000000000000000000f423f$", jsonSent["data"])

}

func TestNewContractDeployTxnSimpleStorageCalcGasFail(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{float64(999999)}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.GasPrice = "789"
	tx, err := NewContractDeployTxn(&msg)
	assert.Nil(err)
	rpc := testRPCClient{}

	rpc.mockError = fmt.Errorf("pop")
	err = tx.Send(&rpc)
	assert.EqualError(err, "Failed to calculate gas for transaction: pop")
}

func TestNewContractDeployMissingCompiledOrSolidity(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Parameters = []interface{}{float64(999999)}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewContractDeployTxn(&msg)
	assert.EqualError(err, "Missing Compliled Code + ABI, or Solidity")
}

func TestNewContractDeployPrecompiledSimpleStorage(t *testing.T) {
	assert := assert.New(t)

	c, _ := CompileContract(simpleStorage, "simplestorage", "")

	var msg kldmessages.DeployContract
	msg.Compiled = c.Compiled
	msg.ABI = &kldbind.ABI{
		ABI: *c.ABI,
	}
	msg.Parameters = []interface{}{float64(999999)}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	tx, err := NewContractDeployTxn(&msg)
	assert.Nil(err)
	rpc := testRPCClient{}

	tx.Send(&rpc)

	assert.Equal("eth_sendTransaction", rpc.capturedMethod)
	jsonBytesSent, _ := json.Marshal(rpc.capturedArgs[0])
	var jsonSent map[string]interface{}
	json.Unmarshal(jsonBytesSent, &jsonSent)
	assert.Equal("0x7b", jsonSent["nonce"])
	assert.Equal("0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c", jsonSent["from"])
	assert.Equal("0x1c8", jsonSent["gas"])
	assert.Equal("0x0", jsonSent["gasPrice"])
	assert.Equal("0x315", jsonSent["value"])
	// The bytecode has the packed parameters appended to the end
	assert.Regexp(".+00000000000000000000000000000000000000000000000000000000000f423f$", jsonSent["data"])

}

func TestNewContractDeployTxnBadNonce(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{float64(999999)}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "abc"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewContractDeployTxn(&msg)
	assert.Regexp("Converting supplied 'nonce' to integer", err.Error())
}

func TestNewContractDeployBadValue(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{float64(999999)}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "zzz"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewContractDeployTxn(&msg)
	assert.Regexp("Converting supplied 'value' to big integer", err.Error())
}

func TestNewContractDeployBadGas(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{float64(999999)}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "111"
	msg.Gas = "abc"
	msg.GasPrice = "789"
	_, err := NewContractDeployTxn(&msg)
	assert.Regexp("Converting supplied 'gas' to integer", err.Error())
}

func TestNewContractDeployBadGasPrice(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{float64(999999)}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "111"
	msg.Gas = "456"
	msg.GasPrice = "abc"
	_, err := NewContractDeployTxn(&msg)
	assert.Regexp("Converting supplied 'gasPrice' to big integer", err.Error())
}

func TestNewContractDeployTxnBadContract(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = "badness"
	_, err := NewContractDeployTxn(&msg)
	assert.Regexp("Solidity compilation failed", err.Error())
}

func TestNewContractDeployStringForNumber(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{"123"}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewContractDeployTxn(&msg)
	assert.Nil(err)
}

func TestNewContractDeployTxnBadContractName(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.ContractName = "wrongun"
	_, err := NewContractDeployTxn(&msg)
	assert.Regexp("Contract '<stdin>:wrongun' not found in Solidity source", err.Error())
}
func TestNewContractDeploySpecificContractName(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = twoContracts
	msg.ContractName = "contract1"
	msg.Parameters = []interface{}{}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewContractDeployTxn(&msg)
	assert.Nil(err)
}

func TestNewContractDeployMissingNameMultipleContracts(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = twoContracts
	_, err := NewContractDeployTxn(&msg)
	assert.Regexp("More than one contract in Solidity file", err.Error())
}

func TestNewContractDeployBadNumber(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{"ABCD"}
	_, err := NewContractDeployTxn(&msg)
	assert.Regexp("Could not be converted to a number", err.Error())
}

func TestNewContractDeployBadTypeForNumber(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{false}
	_, err := NewContractDeployTxn(&msg)
	assert.Regexp("Must supply a number or a string", err.Error())
}

func TestNewContractDeployMissingParam(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{}
	_, err := NewContractDeployTxn(&msg)
	assert.Regexp("Requires 1 args \\(supplied=0\\)", err.Error())
}

func testComplexParam(t *testing.T, solidityType string, val interface{}, expectedErr string) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = "pragma solidity >=0.4.22 <0.6.0; contract test {constructor(" + solidityType + " p1) public {}}"
	msg.Parameters = []interface{}{val}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewContractDeployTxn(&msg)

	if expectedErr == "" {
		assert.Nil(err)
	} else if err == nil {
		assert.Fail("Error expected")
	} else {
		assert.Regexp(expectedErr, err.Error())
	}
}

func TestSolidityUIntParamConversion(t *testing.T) {
	testComplexParam(t, "uint8", float64(123), "")
	testComplexParam(t, "uint8", "123", "")
	testComplexParam(t, "uint16", float64(123), "")
	testComplexParam(t, "uint16", "123", "")
	testComplexParam(t, "uint32", float64(123), "")
	testComplexParam(t, "uint32", "123", "")
	testComplexParam(t, "uint64", float64(123), "")
	testComplexParam(t, "uint64", "123", "")
	testComplexParam(t, "uint64", false, "Must supply a number or a string")
	testComplexParam(t, "uint24", float64(123), "")
	testComplexParam(t, "uint24", "123", "")
	testComplexParam(t, "uint256", float64(123), "")
	testComplexParam(t, "uint256", "123", "")
	testComplexParam(t, "uint256", true, "Must supply a number or a string")
	testComplexParam(t, "uint256", "abc", "Could not be converted to a number")
}
func TestSolidityIntParamConversion(t *testing.T) {
	testComplexParam(t, "int8", float64(123), "")
	testComplexParam(t, "int8", "123", "")
	testComplexParam(t, "int16", float64(123), "")
	testComplexParam(t, "int16", "123", "")
	testComplexParam(t, "int32", float64(123), "")
	testComplexParam(t, "int32", "123", "")
	testComplexParam(t, "int64", float64(123), "")
	testComplexParam(t, "int64", "123", "")
	testComplexParam(t, "int64", false, "Must supply a number or a string")
	testComplexParam(t, "int24", float64(123), "")
	testComplexParam(t, "int24", "123", "")
	testComplexParam(t, "int256", float64(123), "")
	testComplexParam(t, "int256", "123", "")
	testComplexParam(t, "int256", true, "Must supply a number or a string")
	testComplexParam(t, "int256", "abc", "Could not be converted to a number")
}

func TestSolidityIntArrayParamConversion(t *testing.T) {
	testComplexParam(t, "int8[] memory", []float64{123, 456, 789}, "")
	testComplexParam(t, "int8[] memory", []float64{}, "")
	testComplexParam(t, "int256[] memory", []float64{123, 456, 789}, "")
	testComplexParam(t, "int256[] memory", []float64{}, "")
	testComplexParam(t, "int256[] memory", float64(123), "Must supply an array")
	testComplexParam(t, "uint8[] memory", []string{"123"}, "")
	testComplexParam(t, "uint8[] memory", []string{"abc"}, "Could not be converted to a number")
}

func TestSolidityStringParamConversion(t *testing.T) {
	testComplexParam(t, "string memory", "ok", "")
	testComplexParam(t, "string memory", float64(5), "Must supply a string")
}

func TestSolidityBoolParamConversion(t *testing.T) {
	testComplexParam(t, "bool", true, "")
	testComplexParam(t, "bool", "true", "")
	testComplexParam(t, "bool", float64(5), "Must supply a boolean or a string")
}

func TestSolidityAddressParamConversion(t *testing.T) {
	testComplexParam(t, "address", float64(123), "Must supply a hex address string")
	testComplexParam(t, "address", "123", "Could not be converted to a hex address")
	testComplexParam(t, "address", "0xff", "Could not be converted to a hex address")
	testComplexParam(t, "address", "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c", "")
}

func TestSolidityBytesParamConversion(t *testing.T) {
	testComplexParam(t, "bytes32", float64(123), "Must supply a hex string")
	testComplexParam(t, "bytes1", "0f", "")
	testComplexParam(t, "bytes4", "0xfeedbeef", "")
	testComplexParam(t, "bytes1", "", "cannot use \\[0\\]uint8 as type \\[1\\]uint8 as argument")
	testComplexParam(t, "bytes16", "0xAA983AD2a0", "cannot use \\[5\\]uint8 as type \\[16\\]uint8 as argument")
}

func TestTypeNotYetSupported(t *testing.T) {
	assert := assert.New(t)
	var tx Txn
	var m abi.Method
	m.Inputs = append(m.Inputs, abi.Argument{Name: "random", Type: abi.Type{Type: reflect.TypeOf(t)}})
	_, err := tx.generateTypedArgs([]interface{}{"abc"}, &m)
	assert.Regexp("Type '.*' is not yet supported", err)
}

func TestSendTxnABIParam(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{"123", float64(123), "abc", "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"}
	msg.Method = &kldmessages.ABIMethod{
		Name: "testFunc",
		Inputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "param1",
				Type: "uint8",
			},
			kldmessages.ABIParam{
				Name: "param2",
				Type: "int256",
			},
			kldmessages.ABIParam{
				Name: "param3",
				Type: "string",
			},
			kldmessages.ABIParam{
				Name: "param4",
				Type: "address",
			},
		},
		Outputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "ret1",
				Type: "uint256",
			},
		},
	}
	msg.To = "0x2b8c0ECc76d0759a8F50b2E14A6881367D805832"
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	tx, err := NewSendTxn(&msg)
	assert.Nil(err)
	msgBytes, _ := json.Marshal(&msg)
	log.Infof(string(msgBytes))

	rpc := testRPCClient{}

	tx.Send(&rpc)
	assert.Equal("eth_sendTransaction", rpc.capturedMethod)
	jsonBytesSent, _ := json.Marshal(rpc.capturedArgs[0])
	var jsonSent map[string]interface{}
	json.Unmarshal(jsonBytesSent, &jsonSent)
	assert.Equal("0x7b", jsonSent["nonce"])
	assert.Equal("0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c", jsonSent["from"])
	assert.Equal("0x1c8", jsonSent["gas"])
	assert.Equal("0x0", jsonSent["gasPrice"])
	assert.Equal("0x315", jsonSent["value"])
	assert.Regexp("0xe5537abb000000000000000000000000000000000000000000000000000000000000007b000000000000000000000000000000000000000000000000000000000000007b0000000000000000000000000000000000000000000000000000000000000080000000000000000000000000aa983ad2a0e0ed8ac639277f37be42f2a5d2618c00000000000000000000000000000000000000000000000000000000000000036162630000000000000000000000000000000000000000000000000000000000", jsonSent["data"])
}

func TestSendTxnInlineParam(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{}

	param1 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param1)
	param1["type"] = "uint8"
	param1["value"] = "123"

	param2 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param2)
	param2["type"] = "int256"
	param2["value"] = float64(123)

	param3 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param3)
	param3["type"] = "string"
	param3["value"] = "abc"

	param4 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param4)
	param4["type"] = "address"
	param4["value"] = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"

	msg.MethodName = "testFunc"
	msg.To = "0x2b8c0ECc76d0759a8F50b2E14A6881367D805832"
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	tx, err := NewSendTxn(&msg)
	assert.Nil(err)
	msgBytes, _ := json.Marshal(&msg)
	log.Infof(string(msgBytes))

	rpc := testRPCClient{}

	tx.Send(&rpc)
	assert.Equal("eth_sendTransaction", rpc.capturedMethod)
	jsonBytesSent, _ := json.Marshal(rpc.capturedArgs[0])
	var jsonSent map[string]interface{}
	json.Unmarshal(jsonBytesSent, &jsonSent)
	assert.Equal("0x7b", jsonSent["nonce"])
	assert.Equal("0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c", jsonSent["from"])
	assert.Equal("0x1c8", jsonSent["gas"])
	assert.Equal("0x0", jsonSent["gasPrice"])
	assert.Equal("0x315", jsonSent["value"])
	assert.Regexp("0xe5537abb000000000000000000000000000000000000000000000000000000000000007b000000000000000000000000000000000000000000000000000000000000007b0000000000000000000000000000000000000000000000000000000000000080000000000000000000000000aa983ad2a0e0ed8ac639277f37be42f2a5d2618c00000000000000000000000000000000000000000000000000000000000000036162630000000000000000000000000000000000000000000000000000000000", jsonSent["data"])
}

func TestCallMethod(t *testing.T) {
	assert := assert.New(t)

	params := []interface{}{}

	param1 := make(map[string]interface{})
	params = append(params, param1)
	param1["type"] = "uint8"
	param1["value"] = "123"

	param2 := make(map[string]interface{})
	params = append(params, param2)
	param2["type"] = "int256"
	param2["value"] = float64(123)

	param3 := make(map[string]interface{})
	params = append(params, param3)
	param3["type"] = "string"
	param3["value"] = "abc"

	param4 := make(map[string]interface{})
	params = append(params, param4)
	param4["type"] = "address"
	param4["value"] = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"

	method := &abi.Method{}
	method.Name = "testFunc"

	rpc := &testRPCClient{}

	_, err := CallMethod(rpc,
		"0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c",
		"0x2b8c0ECc76d0759a8F50b2E14A6881367D805832",
		json.Number("12345"), method, params)
	assert.NoError(err)

	assert.Equal("eth_call", rpc.capturedMethod)
	jsonBytesSent, _ := json.Marshal(rpc.capturedArgs[0])
	var jsonSent map[string]interface{}
	json.Unmarshal(jsonBytesSent, &jsonSent)
	assert.Equal(nil, jsonSent["nonce"])
	assert.Equal("0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c", jsonSent["from"])
	assert.Equal(nil, jsonSent["gas"])
	assert.Equal("0x0", jsonSent["gasPrice"])
	assert.Equal("0x3039", jsonSent["value"])
	assert.Regexp("0xe5537abb000000000000000000000000000000000000000000000000000000000000007b000000000000000000000000000000000000000000000000000000000000007b0000000000000000000000000000000000000000000000000000000000000080000000000000000000000000aa983ad2a0e0ed8ac639277f37be42f2a5d2618c00000000000000000000000000000000000000000000000000000000000000036162630000000000000000000000000000000000000000000000000000000000", jsonSent["data"])
}

func TestCallMethodFail(t *testing.T) {
	assert := assert.New(t)

	params := []interface{}{}

	method := &abi.Method{}
	method.Name = "testFunc"

	rpc := &testRPCClient{
		mockError: fmt.Errorf("pop"),
	}

	_, err := CallMethod(rpc,
		"0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c",
		"0x2b8c0ECc76d0759a8F50b2E14A6881367D805832",
		json.Number("12345"), method, params)

	assert.Equal("eth_call", rpc.capturedMethod)
	assert.EqualError(err, "Call failed: pop")
}

func TestCallMethodBadArgs(t *testing.T) {
	assert := assert.New(t)

	rpc := &testRPCClient{
		mockError: fmt.Errorf("pop"),
	}

	_, err := CallMethod(rpc, "badness", "", json.Number(""), &abi.Method{}, []interface{}{})

	assert.EqualError(err, "Supplied value for 'from' is not a valid hex address")
}

func TestSendTxnNodeAssignNonce(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{}

	param1 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param1)
	param1["type"] = "uint8"
	param1["value"] = "123"

	param2 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param2)
	param2["type"] = "int256"
	param2["value"] = float64(123)

	param3 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param3)
	param3["type"] = "string"
	param3["value"] = "abc"

	param4 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param4)
	param4["type"] = "address"
	param4["value"] = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"

	msg.MethodName = "testFunc"
	msg.To = "0x2b8c0ECc76d0759a8F50b2E14A6881367D805832"
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	tx, err := NewSendTxn(&msg)
	assert.Nil(err)
	msgBytes, _ := json.Marshal(&msg)
	log.Infof(string(msgBytes))

	rpc := testRPCClient{}

	tx.NodeAssignNonce = true
	tx.Send(&rpc)
	assert.Equal("eth_sendTransaction", rpc.capturedMethod)
	jsonBytesSent, _ := json.Marshal(rpc.capturedArgs[0])
	var jsonSent map[string]interface{}
	json.Unmarshal(jsonBytesSent, &jsonSent)
	assert.Equal(nil, jsonSent["nonce"])
	assert.Equal("0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c", jsonSent["from"])
	assert.Equal("0x1c8", jsonSent["gas"])
	assert.Equal("0x0", jsonSent["gasPrice"])
	assert.Equal("0x315", jsonSent["value"])
	assert.Regexp("0xe5537abb000000000000000000000000000000000000000000000000000000000000007b000000000000000000000000000000000000000000000000000000000000007b0000000000000000000000000000000000000000000000000000000000000080000000000000000000000000aa983ad2a0e0ed8ac639277f37be42f2a5d2618c00000000000000000000000000000000000000000000000000000000000000036162630000000000000000000000000000000000000000000000000000000000", jsonSent["data"])
}

func TestSendTxnInlineBadParamType(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{}

	param1 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param1)
	param1["type"] = "badness"
	param1["value"] = "123"

	msg.Method = &kldmessages.ABIMethod{
		Name: "testFunc",
	}
	msg.To = "0x2b8c0ECc76d0759a8F50b2E14A6881367D805832"
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewSendTxn(&msg)
	assert.Regexp("Param 0: Unable to map badness to etherueum type", err.Error())
}

func TestSendTxnInlineMissingParamType(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{}

	param1 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param1)
	param1["value"] = "123"

	msg.Method = &kldmessages.ABIMethod{
		Name: "testFunc",
	}
	msg.To = "0x2b8c0ECc76d0759a8F50b2E14A6881367D805832"
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewSendTxn(&msg)
	assert.Regexp("Param 0: supplied as an object must have 'type' and 'value' fields", err.Error())
}

func TestSendTxnInlineMissingParamValue(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{}

	param1 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param1)
	param1["type"] = "uint256"

	msg.Method = &kldmessages.ABIMethod{
		Name: "testFunc",
	}
	msg.To = "0x2b8c0ECc76d0759a8F50b2E14A6881367D805832"
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewSendTxn(&msg)
	assert.Regexp("Param 0: supplied as an object must have 'type' and 'value' fields", err.Error())
}

func TestSendTxnInlineBadTypeType(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{}

	param1 := make(map[string]interface{})
	msg.Parameters = append(msg.Parameters, param1)
	param1["type"] = false
	param1["value"] = "abcde"

	msg.Method = &kldmessages.ABIMethod{
		Name: "testFunc",
	}
	msg.To = "0x2b8c0ECc76d0759a8F50b2E14A6881367D805832"
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewSendTxn(&msg)
	assert.Regexp("Param 0: supplied as an object must be string", err.Error())
}
func TestSendTxnBadInputType(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Method = &kldmessages.ABIMethod{
		Name: "testFunc",
		Inputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "param1",
				Type: "badness",
			},
		},
		Outputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "ret1",
				Type: "uint256",
			},
		},
	}
	_, err := NewSendTxn(&msg)
	assert.Regexp("ABI input 0: Unable to map param1 to etherueum type: unsupported arg type:", err.Error())
}

func TestSendTxnMissingMethod(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{"123"}
	msg.Method = &kldmessages.ABIMethod{}
	msg.To = "0x2b8c0ECc76d0759a8F50b2E14A6881367D805832"
	msg.From = "abc"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewSendTxn(&msg)
	assert.Regexp("Method missing", err.Error())
}
func TestSendTxnBadFrom(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{"123"}
	msg.Method = &kldmessages.ABIMethod{
		Name: "testFunc",
		Inputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "param1",
				Type: "uint8",
			},
		},
		Outputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "ret1",
				Type: "uint256",
			},
		},
	}
	msg.To = "0x2b8c0ECc76d0759a8F50b2E14A6881367D805832"
	msg.From = "abc"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewSendTxn(&msg)
	assert.Regexp("Supplied value for 'from' is not a valid hex address", err.Error())
}

func TestSendTxnBadTo(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{"123"}
	msg.Method = &kldmessages.ABIMethod{
		Name: "testFunc",
		Inputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "param1",
				Type: "uint8",
			},
		},
		Outputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "ret1",
				Type: "uint256",
			},
		},
	}
	msg.To = "abc"
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Value = "0"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewSendTxn(&msg)
	assert.Regexp("Supplied value for 'to' is not a valid hex address", err.Error())
}

func TestSendTxnBadOutputType(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Method = &kldmessages.ABIMethod{
		Name: "testFunc",
		Inputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "param1",
				Type: "uint256",
			},
		},
		Outputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "ret1",
				Type: "badness",
			},
		},
	}
	_, err := NewSendTxn(&msg)
	assert.Regexp("ABI output 0: Unable to map ret1 to etherueum type: unsupported arg type:", err.Error())
}

func TestSendBadParams(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{"abc"}
	msg.Method = &kldmessages.ABIMethod{
		Name: "testFunc",
		Inputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "param1",
				Type: "int8",
			},
		},
		Outputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "ret1",
				Type: "uint256",
			},
		},
	}
	_, err := NewSendTxn(&msg)
	assert.Regexp("param 0: Could not be converted to a number", err.Error())
}

func TestSendTxnPackError(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.SendTransaction
	msg.Parameters = []interface{}{""}
	msg.Method = &kldmessages.ABIMethod{
		Name: "testFunc",
		Inputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "param1",
				Type: "bytes1",
			},
		},
		Outputs: []kldmessages.ABIParam{
			kldmessages.ABIParam{
				Name: "ret1",
				Type: "uint256",
			},
		},
	}
	_, err := NewSendTxn(&msg)
	assert.Regexp("cannot use \\[0\\]uint8 as type \\[1\\]uint8 as argument", err.Error())
}

func TestProcessRLPBytesValidTypes(t *testing.T) {
	assert := assert.New(t)

	t1, _ := abi.NewType("string")
	t2, _ := abi.NewType("int256[]")
	t3, _ := abi.NewType("bool")
	t4, _ := abi.NewType("bytes1")
	t5, _ := abi.NewType("address")
	t6, _ := abi.NewType("bytes4")
	t7, _ := abi.NewType("uint256")
	t8, _ := abi.NewType("int32[]")
	t9, _ := abi.NewType("uint32[]")
	methodABI := &abi.Method{
		Name:   "echoTypes2",
		Inputs: []abi.Argument{},
		Outputs: []abi.Argument{
			abi.Argument{Name: "retval1", Type: t1},
			abi.Argument{Name: "retval2", Type: t2},
			abi.Argument{Name: "retval3", Type: t3},
			abi.Argument{Name: "retval4", Type: t4},
			abi.Argument{Name: "retval5", Type: t5},
			abi.Argument{Name: "retval6", Type: t6},
			abi.Argument{Name: "retval7", Type: t7},
			abi.Argument{Name: "retval8", Type: t8},
			abi.Argument{Name: "retval9", Type: t9},
		},
	}
	rlp, err := methodABI.Outputs.Pack(
		"string 1",
		[]*big.Int{big.NewInt(123)},
		true,
		[1]byte{18},
		[20]byte{18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18},
		[4]byte{18, 18, 18, 18},
		big.NewInt(12345),
		[2]int32{-123, -456},
		[2]uint32{123, 456},
	)
	assert.NoError(err)

	res, err := processRLPBytes(methodABI.Outputs, rlp)
	assert.NoError(err)
	assert.Nil(res["error"])

	assert.Equal("string 1", res["retval1"])
	assert.Equal(1, len(res["retval2"].([]interface{})))
	assert.Equal("123", res["retval2"].([]interface{})[0])
	assert.Equal(true, res["retval3"])
	assert.Equal("0x12", res["retval4"])
	assert.Equal("0x1212121212121212121212121212121212121212", res["retval5"])
	assert.Equal("0x12121212", res["retval6"])
	assert.Equal("12345", res["retval7"])
	assert.Equal("-123", res["retval8"].([]interface{})[0])
	assert.Equal("-456", res["retval8"].([]interface{})[1])
	assert.Equal("123", res["retval9"].([]interface{})[0])
	assert.Equal("456", res["retval9"].([]interface{})[1])
}

func TestProcessRLPBytesInvalidNumber(t *testing.T) {
	assert := assert.New(t)

	t1, _ := abi.NewType("int32")
	_, err := mapOutput("test1", "int256", &t1, "not an int")
	assert.EqualError(err, "Expected number type in JSON/RPC response for test1 (int256). Received string")
}

func TestProcessRLPBytesInvalidBool(t *testing.T) {
	assert := assert.New(t)

	t1, _ := abi.NewType("bool")
	_, err := mapOutput("test1", "bool", &t1, "not a bool")
	assert.EqualError(err, "Expected boolean in JSON/RPC response for test1 (bool). Received string")
}

func TestProcessRLPBytesInvalidString(t *testing.T) {
	assert := assert.New(t)

	t1, _ := abi.NewType("string")
	_, err := mapOutput("test1", "string", &t1, 42)
	assert.EqualError(err, "Expected string array in JSON/RPC response for test1 (string). Received int")
}

func TestProcessRLPBytesInvalidByteArray(t *testing.T) {
	assert := assert.New(t)

	t1, _ := abi.NewType("address")
	_, err := mapOutput("test1", "address", &t1, 42)
	assert.EqualError(err, "Expected []byte array in JSON/RPC response for test1 (address). Received int")
}

func TestProcessRLPBytesInvalidArray(t *testing.T) {
	assert := assert.New(t)

	t1, _ := abi.NewType("int32[]")
	_, err := mapOutput("test1", "int32[]", &t1, 42)
	assert.EqualError(err, "Expected slice in JSON/RPC response for test1 (int32[]). Received int")
}

func TestProcessRLPBytesInvalidArrayType(t *testing.T) {
	assert := assert.New(t)

	t1, _ := abi.NewType("int32[]")
	_, err := mapOutput("test1", "int32[]", &t1, []string{"wrong"})
	assert.EqualError(err, "Expected number type in JSON/RPC response for test1 (int32[]). Received string")
}

func TestProcessRLPBytesInvalidTypeByte(t *testing.T) {
	assert := assert.New(t)

	t1, _ := abi.NewType("bool")
	t1.T = 42
	_, err := mapOutput("test1", "randomness", &t1, 42)
	assert.EqualError(err, "Unable to process type for test1 (randomness). Received int")
}

func TestProcessRLPBytesUnpackFailure(t *testing.T) {
	assert := assert.New(t)

	t1, _ := abi.NewType("string")
	methodABI := &abi.Method{
		Name:   "echoTypes2",
		Inputs: []abi.Argument{},
		Outputs: []abi.Argument{
			abi.Argument{Name: "retval1", Type: t1},
		},
	}

	_, err := processRLPBytes(methodABI.Outputs, []byte("this is not the RLP you are looking for"))
	assert.Regexp("cannot marshal", err.Error())
}

func TestProcessOutputsTooFew(t *testing.T) {
	assert := assert.New(t)

	t1, _ := abi.NewType("string")
	methodABI := &abi.Method{
		Name:   "echoTypes2",
		Inputs: []abi.Argument{},
		Outputs: []abi.Argument{
			abi.Argument{Name: "retval1", Type: t1},
		},
	}

	err := processOutputs(methodABI.Outputs, []interface{}{}, make(map[string]interface{}))
	assert.EqualError(err, "Expected 0 in JSON/RPC response. Received 1: []")
}

func TestProcessOutputsTooMany(t *testing.T) {
	assert := assert.New(t)

	methodABI := &abi.Method{
		Name:    "echoTypes2",
		Inputs:  []abi.Argument{},
		Outputs: []abi.Argument{},
	}

	err := processOutputs(methodABI.Outputs, []interface{}{"arg1"}, make(map[string]interface{}))
	assert.EqualError(err, "Expected nil in JSON/RPC response. Received: [arg1]")
}

func TestProcessOutputsBadArgs(t *testing.T) {
	assert := assert.New(t)

	t1, _ := abi.NewType("int32[]")
	methodABI := &abi.Method{
		Name:   "echoTypes2",
		Inputs: []abi.Argument{},
		Outputs: []abi.Argument{
			abi.Argument{Name: "retval1", Type: t1},
		},
	}

	err := processOutputs(methodABI.Outputs, []interface{}{"arg1"}, make(map[string]interface{}))
	assert.EqualError(err, "Expected slice in JSON/RPC response for retval1 (int32[]). Received string")
}
