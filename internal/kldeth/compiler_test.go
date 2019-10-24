// Copyright 2019 Kaleido

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
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common/compiler"
	"github.com/stretchr/testify/assert"
)

func TestPackContractRemovePrefix(t *testing.T) {
	assert := assert.New(t)
	contract := &compiler.Contract{
		Code: "0x00",
	}
	compiled, err := packContract("<stdin>:stuff:watsit", contract)
	assert.NoError(err)
	assert.Equal("watsit", compiled.ContractName)
}

func TestPackContractNoPrefix(t *testing.T) {
	assert := assert.New(t)
	contract := &compiler.Contract{
		Code: "0x00",
	}
	compiled, err := packContract("thingymobob", contract)
	assert.NoError(err)
	assert.Equal("thingymobob", compiled.ContractName)
}

func TestPackContractFailBadHexCode(t *testing.T) {
	assert := assert.New(t)
	contract := &compiler.Contract{
		Code: "Not Hex",
	}
	_, err := packContract("", contract)
	assert.EqualError(err, "Decoding bytecode: hex string without 0x prefix")
}

func TestPackContractFailMarshalABI(t *testing.T) {
	assert := assert.New(t)
	contract := &compiler.Contract{
		Code: "0x00",
		Info: compiler.ContractInfo{
			AbiDefinition: make(map[bool]bool),
		},
	}
	_, err := packContract("", contract)
	assert.EqualError(err, "Serializing ABI: json: unsupported type: map[bool]bool")
}

func TestPackContractFailUnmarshalABIJSON(t *testing.T) {
	assert := assert.New(t)
	contract := &compiler.Contract{
		Code: "0x00",
		Info: compiler.ContractInfo{
			AbiDefinition: map[string]string{
				"not": "an ABI",
			},
		},
	}
	_, err := packContract("", contract)
	assert.Regexp("Parsing ABI", err)
}

func TestPackContractFailSerializingDevDoc(t *testing.T) {
	assert := assert.New(t)
	contract := &compiler.Contract{
		Code: "0x00",
		Info: compiler.ContractInfo{
			DeveloperDoc: make(map[bool]bool),
		},
	}
	_, err := packContract("", contract)
	assert.Regexp("Serializing DevDoc", err.Error())
}

func TestSolcDefaultVersion(t *testing.T) {
	assert := assert.New(t)
	os.Setenv("KLD_SOLC_DEFAULT", "")
	defaultSolc = ""
	solc, err := getSolcExecutable("")
	assert.NoError(err)
	assert.Equal("solc", solc)
	os.Unsetenv("KLD_SOLC_DEFAULT")
}

func TestSolcDefaultVersionEnvVar(t *testing.T) {
	assert := assert.New(t)
	os.Setenv("KLD_SOLC_DEFAULT", "solc123")
	defaultSolc = ""
	solc, err := getSolcExecutable("")
	assert.NoError(err)
	assert.Equal("solc123", solc)
	os.Unsetenv("KLD_SOLC_DEFAULT")
}

func TestSolcCustomVersionValidMajor(t *testing.T) {
	assert := assert.New(t)
	os.Setenv("KLD_SOLC_0_4", "solc04")
	defaultSolc = ""
	solc, err := getSolcExecutable("0.4")
	assert.NoError(err)
	assert.Equal("solc04", solc)
}

func TestSolcCustomVersionValidMinor(t *testing.T) {
	assert := assert.New(t)
	os.Setenv("KLD_SOLC_0_4", "solc04")
	defaultSolc = ""
	solc, err := getSolcExecutable("0.4.23.some interesting things")
	assert.NoError(err)
	assert.Equal("solc04", solc)
}

func TestSolcCustomVersionUnknown(t *testing.T) {
	assert := assert.New(t)
	defaultSolc = ""
	_, err := getSolcExecutable("0.5")
	assert.EqualError(err, "Could not find a configured compiler for requested Solidity major version 0.5")
}

func TestSolcCustomVersionInvalid(t *testing.T) {
	assert := assert.New(t)
	defaultSolc = ""
	_, err := getSolcExecutable("0.")
	assert.EqualError(err, "Invalid Solidity version requested for compiler. Ensure the string starts with two dot separated numbers, such as 0.5")
}

func TestSolcCompileInvalidVersion(t *testing.T) {
	assert := assert.New(t)
	defaultSolc = ""
	_, err := CompileContract("", "", "zero.four", "")
	assert.EqualError(err, "Invalid Solidity version requested for compiler. Ensure the string starts with two dot separated numbers, such as 0.5")
}
