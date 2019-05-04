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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	log "github.com/sirupsen/logrus"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/compiler"
)

// CompiledSolidity wraps solc compilation of solidity and ABI generation
type CompiledSolidity struct {
	ContractName string
	Compiled     []byte
	DevDoc       string
	ABI          *abi.ABI
	ContractInfo *compiler.ContractInfo
}

var solcVerChecker *regexp.Regexp
var defaultSolc string

// GetSolc returns the appropriate solc command based on the combination of env vars, and message-specific request
// parameters passed in
func GetSolc(requestedVersion string) (string, error) {
	log.Infof("Solidity compiler requested: %s", requestedVersion)
	if solcVerChecker == nil {
		solcVerChecker, _ = regexp.Compile("^([0-9]+)\\.?([0-9]+)")
	}
	if defaultSolc == "" {
		if envVar := os.Getenv("KLD_SOLC_DEFAULT"); envVar != "" {
			defaultSolc = envVar
		} else {
			defaultSolc = "solc"
		}
	}
	solc := defaultSolc
	if v := solcVerChecker.FindStringSubmatch(requestedVersion); v != nil {
		envVarName := "KLD_SOLC_" + v[1] + "_" + v[2]
		if envVar := os.Getenv(envVarName); envVar != "" {
			solc = envVar
		} else {
			return "", fmt.Errorf("Could not find a configured compiler for requested Solidity major version %s.%s", v[1], v[2])
		}
	} else if requestedVersion != "" {
		return "", fmt.Errorf("Invalid Solidity version requested for compiler. Ensure the string starts with two dot separated numbers, such as 0.5")
	}
	log.Debugf("Solidity compiler solc binary: %s", solc)
	return solc, nil
}

// CompileContract uses solc to compile the Solidity source and
func CompileContract(soliditySource, contractName, requestedVersion string) (*CompiledSolidity, error) {
	// Compile the solidity
	solc, err := GetSolc(requestedVersion)
	if err != nil {
		return nil, err
	}
	compiled, err := compiler.CompileSolidityString(solc, soliditySource)
	if err != nil {
		return nil, fmt.Errorf("Solidity compilation failed: %s", err)
	}
	return ProcessCompiled(compiled, contractName, true)
}

// ProcessCompiled takes solc output and packs it into our CompiledSolidity structure
func ProcessCompiled(compiled map[string]*compiler.Contract, contractName string, isStdin bool) (*CompiledSolidity, error) {
	// Get the individual contract we want to deploy
	var contract *compiler.Contract
	contractNames := reflect.ValueOf(compiled).MapKeys()
	if contractName != "" {
		if isStdin {
			contractName = "<stdin>:" + contractName
		}
		if _, ok := compiled[contractName]; !ok {
			return nil, fmt.Errorf("Contract '%s' not found in Solidity source: %s", contractName, contractNames)
		}
		contract = compiled[contractName]
	} else if len(contractNames) != 1 {
		return nil, fmt.Errorf("More than one contract in Solidity file, please set one to call: %s", contractNames)
	} else {
		contractName = contractNames[0].String()
		contract = compiled[contractName]
	}
	return packContract(contractName, contract)
}

func packContract(contractName string, contract *compiler.Contract) (c *CompiledSolidity, err error) {

	firstColon := strings.LastIndex(contractName, ":")
	if firstColon >= 0 && firstColon < (len(contractName)-1) {
		contractName = contractName[firstColon+1:]
	}

	c = &CompiledSolidity{
		ContractName: contractName,
		ContractInfo: &contract.Info,
	}
	c.Compiled, err = hexutil.Decode(contract.Code)
	if err != nil {
		return nil, fmt.Errorf("Decoding bytecode: %s", err)
	}
	if len(c.Compiled) == 0 {
		return nil, fmt.Errorf("Specified contract compiled ok, but did not result in any bytecode: %s", contractName)
	}
	// Pack the arguments for calling the contract
	abiJSON, err := json.Marshal(contract.Info.AbiDefinition)
	if err != nil {
		return nil, fmt.Errorf("Serializing ABI: %s", err)
	}
	abi, err := abi.JSON(bytes.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("Parsing ABI: %s", err)
	}
	c.ABI = &abi
	devdocBytes, err := json.Marshal(contract.Info.DeveloperDoc)
	if err != nil {
		return nil, fmt.Errorf("Serializing DevDoc: %s", err)
	}
	c.DevDoc = string(devdocBytes)
	return c, nil
}
