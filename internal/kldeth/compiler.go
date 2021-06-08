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
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"

	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	"github.com/kaleido-io/ethconnect/internal/eth"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldutils"

	log "github.com/sirupsen/logrus"
)

const (
	// DefaultEVMVersion is the EVMVersion to be used when not specified explicitly
	defaultEVMVersion = "byzantium"
)

// CompiledSolidity wraps solc compilation of solidity and ABI generation
type CompiledSolidity struct {
	ContractName string
	Compiled     []byte
	DevDoc       string
	ABI          ethbinding.ABIMarshaling
	ContractInfo *ethbinding.ContractInfo
}

var solcVerChecker *regexp.Regexp
var defaultSolc string

func getSolcExecutable(requestedVersion string) (string, error) {
	log.Infof("Solidity compiler requested: %s", requestedVersion)
	if solcVerChecker == nil {
		solcVerChecker, _ = regexp.Compile("^([0-9]+)\\.?([0-9]+)")
	}
	defaultSolc = kldutils.GetenvOrDefaultLowerCase(kldutils.GetenvOrDefaultUpperCase("PREFIX_SHORT", "fly")+"_SOLC_DEFAULT", "solc")
	solc := defaultSolc
	if v := solcVerChecker.FindStringSubmatch(requestedVersion); v != nil {
		envVarName := kldutils.GetenvOrDefaultUpperCase("PREFIX_SHORT", "fly") + "_SOLC_" + v[1] + "_" + v[2]
		if envVar := os.Getenv(envVarName); envVar != "" {
			solc = envVar
		} else {
			return "", klderrors.Errorf(klderrors.CompilerVersionNotFound, v[1], v[2])
		}
	} else if requestedVersion != "" {
		return "", klderrors.Errorf(klderrors.CompilerVersionBadRequest)
	}
	log.Debugf("Solidity compiler solc binary: %s", solc)
	return solc, nil
}

// GetSolc returns the appropriate solc command based on the combination of env vars, and message-specific request
// parameters passed in
func GetSolc(requestedVersion string) (*ethbinding.Solidity, error) {
	solc, err := getSolcExecutable(requestedVersion)
	if err != nil {
		return nil, err
	}
	return eth.API.SolidityVersion(solc)
}

// GetSolcArgs get the correct solc args
func GetSolcArgs(evmVersion string) []string {
	if evmVersion == "" {
		evmVersion = defaultEVMVersion
	}
	return []string{
		"--combined-json", "bin,bin-runtime,srcmap,srcmap-runtime,abi,userdoc,devdoc,metadata",
		"--optimize",
		"--evm-version", evmVersion,
		"--allow-paths", ".",
	}
}

// CompileContract uses solc to compile the Solidity source and
func CompileContract(soliditySource, contractName, requestedVersion, evmVersion string) (*CompiledSolidity, error) {
	// Compile the solidity
	s, err := GetSolc(requestedVersion)
	if err != nil {
		return nil, err
	}

	solcArgs := GetSolcArgs(evmVersion)
	cmd := exec.Command(s.Path, append(solcArgs, "--", "-")...)
	cmd.Stdin = strings.NewReader(soliditySource)
	var stderr, stdout bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, klderrors.Errorf(klderrors.CompilerFailedSolc, err, stderr.String())
	}
	c, _ := eth.API.ParseCombinedJSON(stdout.Bytes(), soliditySource, s.Version, s.Version, strings.Join(solcArgs, " "))
	return ProcessCompiled(c, contractName, true)
}

// ProcessCompiled takes solc output and packs it into our CompiledSolidity structure
func ProcessCompiled(compiled map[string]*ethbinding.Contract, contractName string, isStdin bool) (*CompiledSolidity, error) {
	// Get the individual contract we want to deploy
	var contract *ethbinding.Contract
	contractNames := reflect.ValueOf(compiled).MapKeys()
	if contractName != "" {
		if isStdin {
			contractName = "<stdin>:" + contractName
		}
		if _, ok := compiled[contractName]; !ok {
			return nil, klderrors.Errorf(klderrors.CompilerOutputMissingContract, contractName, contractNames)
		}
		contract = compiled[contractName]
	} else if len(contractNames) != 1 {
		return nil, klderrors.Errorf(klderrors.CompilerOutputMultipleContracts, contractNames)
	} else {
		contractName = contractNames[0].String()
		contract = compiled[contractName]
	}
	return packContract(contractName, contract)
}

func packContract(contractName string, contract *ethbinding.Contract) (c *CompiledSolidity, err error) {

	firstColon := strings.LastIndex(contractName, ":")
	if firstColon >= 0 && firstColon < (len(contractName)-1) {
		contractName = contractName[firstColon+1:]
	}

	c = &CompiledSolidity{
		ContractName: contractName,
		ContractInfo: &contract.Info,
	}
	c.Compiled, err = eth.API.HexDecode(contract.Code)
	if err != nil {
		return nil, klderrors.Errorf(klderrors.CompilerBytecodeInvalid, err)
	}
	if len(c.Compiled) == 0 {
		return nil, klderrors.Errorf(klderrors.CompilerBytecodeEmpty, contractName)
	}
	// Pack the arguments for calling the contract
	abiJSON, err := json.Marshal(contract.Info.AbiDefinition)
	if err != nil {
		return nil, klderrors.Errorf(klderrors.CompilerABISerialize, err)
	}
	var abi ethbinding.ABIMarshaling
	err = json.Unmarshal(abiJSON, &abi)
	if err != nil {
		return nil, klderrors.Errorf(klderrors.CompilerABIReRead, err)
	}
	c.ABI = abi
	devdocBytes, err := json.Marshal(contract.Info.DeveloperDoc)
	if err != nil {
		return nil, klderrors.Errorf(klderrors.CompilerSerializeDevDocs, err)
	}
	c.DevDoc = string(devdocBytes)
	return c, nil
}
