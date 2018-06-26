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
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/compiler"
)

// CompiledSolidity wraps solc compilation of solidity and ABI generation
type CompiledSolidity struct {
	Compiled string
	ABI      abi.ABI
}

// CompileContract uses solc to compile the Solidity source and
func CompileContract(soliditySource, contractName string) (*CompiledSolidity, error) {
	var c CompiledSolidity

	// Compile the solidity
	compiled, err := compiler.CompileSolidityString("solc", soliditySource)
	if err != nil {
		return nil, fmt.Errorf("Solidity compilation failed: %s", err)
	}

	// Get the individual contract we want to deploy
	var contract *compiler.Contract
	contractNames := reflect.ValueOf(compiled).MapKeys()
	if contractName != "" {
		if _, ok := compiled[contractName]; !ok {
			return nil, fmt.Errorf("Contract %s not found in Solidity source: %s", contractName, contractNames)
		}
		contract = compiled[contractName]
	} else if len(contractNames) != 1 {
		return nil, fmt.Errorf("More than one contract in Solidity file, please set one to call: %s", contractNames)
	} else {
		contractName = contractNames[0].String()
		contract = compiled[contractName]
	}
	c.Compiled = contract.Code

	// Pack the arguments for calling the contract
	abiJSON, err := json.Marshal(contract.Info.AbiDefinition)
	if err != nil {
		return nil, fmt.Errorf("Serializing ABI: %s", err)
	}
	c.ABI, err = abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("Parsing ABI: %s", err)
	}

	return &c, nil
}
