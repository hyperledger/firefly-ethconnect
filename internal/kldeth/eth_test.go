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
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/stretchr/testify/assert"
)

// Slim interface for stubbing
type testRPCClient struct {
	mockResult     interface{}
	mockError      error
	capturedMethod string
	capturedArgs   []interface{}
}

func (r *testRPCClient) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	r.capturedMethod = method
	r.capturedArgs = args
	result = r.mockResult
	return r.mockError
}

const (
	simpleStorage = "pragma solidity ^0.4.17;\n\ncontract simplestorage {\nuint public storedData;\n\nfunction simplestorage(uint initVal) public {\nstoredData = initVal;\n}\n\nfunction set(uint x) public {\nstoredData = x;\n}\n\nfunction get() public constant returns (uint retVal) {\nreturn storedData;\n}\n}"
	twoContracts  = "pragma solidity ^0.4.17;\n\ncontract contract1 {function f1() public constant returns (uint retVal) {\nreturn 1;\n}\n}\n\ncontract contract2 {function f2() public constant returns (uint retVal) {\nreturn 2;\n}\n}"
)

func TestNewContractDeployTxnSimpleStorage(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{float64(999999)}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Gas = "456"
	msg.GasPrice = "789"
	tx, err := NewContractDeployTxn(msg)
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

func TestNewContractDeployTxnBadContract(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = "badness"
	_, err := NewContractDeployTxn(msg)
	assert.Regexp("Solidity compilation failed", err.Error())
}

func TestNewContractDeployStringForNumber(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{"123"}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewContractDeployTxn(msg)
	assert.Nil(err)
}

func TestNewContractDeployTxnBadContractName(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.ContractName = "wrongun"
	_, err := NewContractDeployTxn(msg)
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
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewContractDeployTxn(msg)
	assert.Nil(err)
}

func TestNewContractDeployMissingNameMultipleContracts(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = twoContracts
	_, err := NewContractDeployTxn(msg)
	assert.Regexp("More than one contract in Solidity file", err.Error())
}

func TestNewContractDeployBadNumber(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{"ABCD"}
	_, err := NewContractDeployTxn(msg)
	assert.Regexp("Could not be converted to a number", err.Error())
}

func TestNewContractDeployBadTypeForNumber(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{false}
	_, err := NewContractDeployTxn(msg)
	assert.Regexp("Must supply a number or a string", err.Error())
}

func TestNewContractDeployMissingParam(t *testing.T) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = simpleStorage
	msg.Parameters = []interface{}{}
	_, err := NewContractDeployTxn(msg)
	assert.Regexp("Requires 1 args \\(supplied=0\\)", err.Error())
}

func testComplexParam(t *testing.T, solidityType string, val interface{}, expectedErr string) {
	assert := assert.New(t)

	var msg kldmessages.DeployContract
	msg.Solidity = "pragma solidity ^0.4.17; contract test {constructor(" + solidityType + " p1) public {}}"
	msg.Parameters = []interface{}{val}
	msg.From = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg.Nonce = "123"
	msg.Gas = "456"
	msg.GasPrice = "789"
	_, err := NewContractDeployTxn(msg)

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

func TestSolidityStringParamConversion(t *testing.T) {
	testComplexParam(t, "string", "ok", "")
	testComplexParam(t, "string", float64(5), "Must supply a string")
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
	var tx KldTx
	var m abi.Method
	m.Inputs = append(m.Inputs, abi.Argument{Name: "random", Type: abi.Type{Type: reflect.TypeOf(t)}})
	_, err := tx.generateTypedArgs([]interface{}{"abc"}, m)
	assert.Regexp("Type '.*' is not yet supported", err)
}
