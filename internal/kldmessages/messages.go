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
)

const (
	// MsgTypeDeployContract - deploy a contract
	MsgTypeDeployContract = "DeployContract"
	// MsgTypeSendTransaction - send a transaction
	MsgTypeSendTransaction = "SendTransaction"
)

// UnmarshalKldMessage is a trivial wrapper around json decoding
func UnmarshalKldMessage(jsonMsg string, msg interface{}) error {
	return json.Unmarshal([]byte(jsonMsg), msg)
}

// ABIFunction is the web3 form for an individual function
// described in https://web3js.readthedocs.io/en/1.0/glossary.html
type ABIFunction struct {
	Type            string     `json:"type,omitempty"`
	Name            string     `json:"name"`
	Constant        bool       `json:"constant"`
	Payable         bool       `json:"payable,omitempty"`
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
	Nonce      json.Number   `json:"nonce"`
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
	Function ABIFunction `json:"function"`
}

// DeployContract message instructs the bridge to install a contract
type DeployContract struct {
	transactionCommon
	Solidity     string `json:"solidity"`
	ContractName string `json:"contractName,omitempty"`
}
