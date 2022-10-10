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
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strings"

	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/ethbind"
	"github.com/hyperledger/firefly-ethconnect/internal/utils"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	log "github.com/sirupsen/logrus"
)

const (
	// DefaultEVMVersion is the EVMVersion to be used when not specified explicitly
	defaultEVMVersion = "byzantium"
)

type SolcVersion struct {
	Path    string
	Version string
}

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
var solcVerExtractor = regexp.MustCompile(`\d+\.\d+\.\d+`)

func getSolcExecutable(requestedVersion string) (string, error) {
	log.Infof("Solidity compiler requested: %s", requestedVersion)
	if solcVerChecker == nil {
		solcVerChecker, _ = regexp.Compile(`^([0-9]+)\.?([0-9]+)`)
	}
	defaultSolc = utils.GetenvOrDefaultLowerCase(utils.GetenvOrDefaultUpperCase("PREFIX_SHORT", "fly")+"_SOLC_DEFAULT", "solc")
	solc := defaultSolc
	if v := solcVerChecker.FindStringSubmatch(requestedVersion); v != nil {
		envVarName := utils.GetenvOrDefaultUpperCase("PREFIX_SHORT", "fly") + "_SOLC_" + v[1] + "_" + v[2]
		if envVar := os.Getenv(envVarName); envVar != "" {
			solc = envVar
		} else {
			return "", errors.Errorf(errors.CompilerVersionNotFound, v[1], v[2])
		}
	} else if requestedVersion != "" {
		return "", errors.Errorf(errors.CompilerVersionBadRequest)
	}
	log.Debugf("Solidity compiler solc binary: %s", solc)
	return solc, nil
}

func getSolcVersion(solcPath string) (*SolcVersion, error) {

	cmdOutput := new(bytes.Buffer)
	cmd := exec.Command(solcPath, "--version")
	cmd.Stdout = cmdOutput

	err := cmd.Run()
	if err != nil {
		return nil, errors.Errorf(errors.CompilerFailedVersion, solcPath, err)
	}

	ver := solcVerExtractor.FindString(cmdOutput.String())
	if ver == "" {
		return nil, errors.Errorf(errors.CompilerFailedVersionRegex, solcPath, cmdOutput.String())
	}

	return &SolcVersion{
		Path:    solcPath,
		Version: ver,
	}, nil

}

// GetSolc returns the appropriate solc command based on the combination of env vars, and message-specific request
// parameters passed in
func GetSolc(requestedVersion string) (*SolcVersion, error) {
	solc, err := getSolcExecutable(requestedVersion)
	if err != nil {
		return nil, err
	}
	return getSolcVersion(solc)
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
		return nil, errors.Errorf(errors.CompilerFailedSolc, err, stderr.String())
	}
	c, _ := ethbind.API.ParseCombinedJSON(stdout.Bytes(), soliditySource, s.Version, s.Version, strings.Join(solcArgs, " "))
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
			return nil, errors.Errorf(errors.CompilerOutputMissingContract, contractName, contractNames)
		}
		contract = compiled[contractName]
	} else if len(contractNames) != 1 {
		return nil, errors.Errorf(errors.CompilerOutputMultipleContracts, contractNames)
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
	c.Compiled, err = ethbind.API.HexDecode(contract.Code)
	if err != nil {
		return nil, errors.Errorf(errors.CompilerBytecodeInvalid, err)
	}
	if len(c.Compiled) == 0 {
		return nil, errors.Errorf(errors.CompilerBytecodeEmpty, contractName)
	}
	// Pack the arguments for calling the contract
	abiJSON, err := json.Marshal(contract.Info.AbiDefinition)
	if err != nil {
		return nil, errors.Errorf(errors.CompilerABISerialize, err)
	}
	var abi ethbinding.ABIMarshaling
	err = json.Unmarshal(abiJSON, &abi)
	if err != nil {
		return nil, errors.Errorf(errors.CompilerABIReRead, err)
	}
	c.ABI = abi
	devdocBytes, err := json.Marshal(contract.Info.DeveloperDoc)
	if err != nil {
		return nil, errors.Errorf(errors.CompilerSerializeDevDocs, err)
	}
	c.DevDoc = string(devdocBytes)
	return c, nil
}
