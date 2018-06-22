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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/kaleido-io/ethconnect/pkg/kldutils"
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

// transactionCommon is the common fields from https://github.com/ethereum/wiki/wiki/JavaScript-API#web3ethsendtransaction
// for sending either contract call or creation transactions
type transactionCommon struct {
	From     string      `json:"from"`
	To       string      `json:"to"`
	Value    json.Number `json:"value"`
	Gas      json.Number `json:"gas"`
	GasPrice json.Number `json:"gasPrice"`
	Nonce    json.Number `json:"nonce"`
}

// TransactionCommon allows conversion to an ethereum transaction from either type of message
type TransactionCommon interface {
	ToEthTransaction(data []byte) (ethTx *types.Transaction, err error)
}

func (t *transactionCommon) ToEthTransaction(data []byte) (ethTx *types.Transaction, err error) {

	nonce, err := t.Nonce.Int64()
	if err != nil {
		err = fmt.Errorf("Converting supplied 'nonce' to integer: %s", err)
		return
	}

	toAddr, err := kldutils.StrToAddress("to", t.To)
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

	ethTx = types.NewTransaction(uint64(nonce), toAddr, value, uint64(gas), gasPrice, data)
	log.Debug("TX:%s To=%s Value=%d Gas=%d GasPrice=%d",
		ethTx.Hash().Hex(), ethTx.To().Hex(), ethTx.Value, ethTx.Gas, ethTx.GasPrice)
	return
}

// SendTransaction message instructs the bridge to install a contract
type SendTransaction struct {
	transactionCommon
	Contract   common.Address `json:"contract"`
	Function   ABIFunction    `json:"function"`
	Parameters []interface{}  `json:"params"`
}

// DeployContract message instructs the bridge to install a contract
type DeployContract struct {
	transactionCommon
	Solidity     string        `json:"solidity"`
	ContractName string        `json:"contractName,omitempty"`
	Parameters   []interface{} `json:"params"`
}
