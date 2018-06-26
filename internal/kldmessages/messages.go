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

package kldmessages

import (
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
)

const (
	// MsgTypeDeployContract - deploy a contract
	MsgTypeDeployContract = "DeployContract"
)

// ABIFunction is the web3 form for an individual function
// described in https://web3js.readthedocs.io/en/1.0/glossary.html
type ABIFunction struct {
	Type            string     `json:"type"`
	Name            string     `json:"name"`
	Constant        bool       `json:"constant"`
	Payable         bool       `json:"payable"`
	StateMutability string     `json:"stateMutability"`
	Inputs          []ABIParam `json:"inputs"`
	Outputs         []ABIParam `json:"outputs"`
}

// ABIParam is an individual function parameter, for input or output, in an ABI
type ABIParam struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// CommonHeaders are common to all messages
type CommonHeaders struct {
	ID      string      `json:"id,omitempty"`
	MsgType string      `json:"type"`
	Account string      `json:"account,omitempty"`
	Context interface{} `json:"ctx,omitempty"`
}

// RequestCommon is a common interface to all requests
type RequestCommon struct {
	Headers CommonHeaders `json:"headers"`
}

// ReplyHeaders are common to all replies
type ReplyHeaders struct {
	CommonHeaders
	OrigMsg      string `json:"origMsg"`
	OrigID       string `json:"origID"`
	OrigTX       string `json:"origTX,omitempty"`
	Status       int32  `json:"status"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// ReplyCommon is a common interface to all replies
type ReplyCommon struct {
	Headers ReplyHeaders `json:"headers"`
}

// transactionCommon is the common fields from https://github.com/ethereum/wiki/wiki/JavaScript-API#web3ethsendtransaction
// for sending either contract call or creation transactions
type transactionCommon struct {
	RequestCommon
	From       string        `json:"from"`
	Value      json.Number   `json:"value"`
	Gas        json.Number   `json:"gas"`
	GasPrice   json.Number   `json:"gasPrice"`
	Parameters []interface{} `json:"params"`
}

// SendTransaction message instructs the bridge to install a contract
type SendTransaction struct {
	transactionCommon
	To       string      `json:"to"`
	Contract string      `json:"contract"`
	Function ABIFunction `json:"function"`
}

// DeployContract message instructs the bridge to install a contract
type DeployContract struct {
	transactionCommon
	Solidity     string `json:"solidity"`
	ContractName string `json:"contractName,omitempty"`
}

func (t *transactionCommon) ToEthTransaction(nonce uint64, to string, data []byte) (ethTx *types.Transaction, fromAddr common.Address, err error) {

	fromAddr, err = kldutils.StrToAddress("from", t.From)
	if err != nil {
		return
	}

	value := big.NewInt(0)
	if _, ok := value.SetString(t.Value.String(), 10); !ok {
		err = fmt.Errorf("Converting supplied 'value' to big integer: %s", err)
	}

	gas, err := t.Gas.Int64()
	if err != nil {
		err = fmt.Errorf("Converting supplied 'nonce' to integer: %s", err)
		return
	}

	gasPrice := big.NewInt(0)
	if _, ok := value.SetString(t.Value.String(), 10); !ok {
		err = fmt.Errorf("Converting supplied 'gasPrice' to big integer: %s", err)
	}

	var toAddr common.Address
	if to != "" {
		if toAddr, err = kldutils.StrToAddress("to", to); err != nil {
			return
		}
		ethTx = types.NewTransaction(nonce, toAddr, value, uint64(gas), gasPrice, data)
	} else {
		ethTx = types.NewContractCreation(nonce, value, uint64(gas), gasPrice, data)
	}
	log.Debug("TX:%s To=%s Value=%d Gas=%d GasPrice=%d",
		ethTx.Hash().Hex(), ethTx.To(), ethTx.Value, ethTx.Gas, ethTx.GasPrice)
	return
}

// GenerateTypedArgs parses string arguments into a range of types to pass to the ABI call
func (t *transactionCommon) GenerateTypedArgs(method abi.Method) (typedArgs []interface{}, err error) {

	log.Debug("Parsing args for function: ", method)
	for idx, inputArg := range method.Inputs {
		if idx >= len(t.Parameters) {
			err = fmt.Errorf("Function '%s': Requires %d args (supplied=%d)", method.Name, len(method.Inputs), len(t.Parameters))
			return
		}
		param := t.Parameters[idx]
		requiredType := inputArg.Type.String()
		suppliedType := reflect.TypeOf(param)
		switch requiredType {
		case "string":
			if suppliedType.Kind() == reflect.String {
				typedArgs = append(typedArgs, param.(string))
			} else {
				err = fmt.Errorf("Function '%s' param %d: Must be a string", method.Name, idx)
			}
			break
		case "int256", "uint256":
			if suppliedType.Kind() == reflect.String {
				bigInt := big.NewInt(0)
				if _, ok := bigInt.SetString(param.(string), 10); !ok {
					err = fmt.Errorf("Function '%s' param %d: Could not be converted to a number", method.Name, idx)
					break
				}
				typedArgs = append(typedArgs, bigInt)
			} else if suppliedType.Kind() == reflect.Float64 {
				typedArgs = append(typedArgs, big.NewInt(int64(param.(float64))))
			} else {
				err = fmt.Errorf("Function '%s' param %d is a %s: Must supply a number or a string", method.Name, idx, requiredType)
			}
			break
		case "bool":
			if suppliedType.Kind() == reflect.String {
				typedArgs = append(typedArgs, strings.ToLower(param.(string)) == "true")
			} else if suppliedType.Kind() == reflect.Bool {
				typedArgs = append(typedArgs, param.(bool))
			} else {
				err = fmt.Errorf("Function '%s' param %d is a %s: Must supply a boolean or a string", method.Name, idx, requiredType)
			}
			break
		case "address":
			if suppliedType.Kind() == reflect.String {
				if !common.IsHexAddress(param.(string)) {
					err = fmt.Errorf("Function '%s' param %d: Could not be converted to a hex address", method.Name, idx)
					break
				}
				typedArgs = append(typedArgs, common.HexToAddress(param.(string)))
			} else {
				err = fmt.Errorf("Function '%s' param %d is a %s: Must supply a boolean or a string", method.Name, idx, requiredType)
			}
			break
		default:
			return nil, fmt.Errorf("Type %s is not yet supported", inputArg.Type)
		}
		if err != nil {
			log.Errorf("%s [Required=%s Supplied=%s Value=%s]", err, requiredType, suppliedType, param)
			return
		}
	}

	return
}
