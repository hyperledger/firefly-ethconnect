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

package kldmessages

import (
	"encoding/json"
	"reflect"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/kaleido-io/ethconnect/internal/kldbind"
)

const (
	// MsgTypeError - an error
	MsgTypeError = "Error"
	// MsgTypeDeployContract - deploy a contract
	MsgTypeDeployContract = "DeployContract"
	// MsgTypeSendTransaction - send a transaction
	MsgTypeSendTransaction = "SendTransaction"
	// MsgTypeTransactionSuccess - a transaction receipt where status is 1
	MsgTypeTransactionSuccess = "TransactionSuccess"
	// MsgTypeTransactionFailure - a transaction receipt where status is 0
	MsgTypeTransactionFailure = "TransactionFailure"
	// RecordHeaderAccessToken - record header name for passing JWT token over messaging
	RecordHeaderAccessToken = "kld-accesstoken"
)

// ABIMethod is the web3 form for an individual function
// described in https://web3js.readthedocs.io/en/1.0/glossary.html
type ABIMethod struct {
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

// AsyncSentMsg is a standard response for async requests
type AsyncSentMsg struct {
	Sent    bool   `json:"sent"`
	Request string `json:"id"`
	Msg     string `json:"msg,omitempty"`
}

// CommonHeaders are common to all messages
type CommonHeaders struct {
	ID      string                 `json:"id,omitempty"`
	MsgType string                 `json:"type"`
	Account string                 `json:"account,omitempty"`
	Context map[string]interface{} `json:"ctx,omitempty"`
}

// RequestCommon is a common interface to all requests
type RequestCommon struct {
	Headers RequestHeaders `json:"headers"`
}

// RequestHeaders are common to all replies
type RequestHeaders struct {
	CommonHeaders
}

// ReplyHeaders are common to all replies
type ReplyHeaders struct {
	CommonHeaders
	Received  string  `json:"timeReceived"`
	Elapsed   float64 `json:"timeElapsed"`
	ReqOffset string  `json:"requestOffset"`
	ReqID     string  `json:"requestId"`
}

// ReplyWithHeaders gives common access the reply headers
type ReplyWithHeaders interface {
	ReplyHeaders() *ReplyHeaders
	IsReceipt() *TransactionReceipt
}

// ReplyCommon is a common interface to all replies
type ReplyCommon struct {
	Headers ReplyHeaders `json:"headers"`
}

// ReplyHeaders returns the reply headers
func (r *ReplyCommon) ReplyHeaders() *ReplyHeaders {
	return &r.Headers
}

// IsReceipt default is nil
func (r *ReplyCommon) IsReceipt() *TransactionReceipt {
	return nil
}

// IsReceipt returns as receipt
func (r *TransactionReceipt) IsReceipt() *TransactionReceipt {
	return r
}

// TransactionCommon is the common fields from https://github.com/ethereum/wiki/wiki/JavaScript-API#web3ethsendtransaction
// for sending either contract call or creation transactions, with eea extensions for private transactions
// from https://entethalliance.github.io/client-spec/spec.html#sec-eea-sendTransaction
// TODO - do Orion/Tessera support "unrestricted" private transactions?
type TransactionCommon struct {
	RequestCommon
	Nonce          json.Number   `json:"nonce,omitempty"`
	From           string        `json:"from"`
	Value          json.Number   `json:"value"`
	Gas            json.Number   `json:"gas"`
	GasPrice       json.Number   `json:"gasPrice"`
	Parameters     []interface{} `json:"params"`
	PrivateFrom    string        `json:"privateFrom,omitempty"`
	PrivateFor     []string      `json:"privateFor,omitempty"`
	PrivacyGroupID string        `json:"privacyGroupId,omitempty"`
}

// SendTransaction message instructs the bridge to install a contract
type SendTransaction struct {
	TransactionCommon
	To         string     `json:"to"`
	Method     *ABIMethod `json:"method,omitempty"`
	MethodName string     `json:"methodName,omitempty"`
}

// DeployContract message instructs the bridge to install a contract
type DeployContract struct {
	TransactionCommon
	Solidity        string       `json:"solidity,omitempty"`
	CompilerVersion string       `json:"compilerVersion,omitempty"`
	EVMVersion      string       `json:"evmVersion,omitempty"`
	ABI             *kldbind.ABI `json:"abi,omitempty"`
	DevDoc          string       `json:"devDocs,omitempty"`
	Compiled        []byte       `json:"compiled,omitempty"`
	ContractName    string       `json:"contractName,omitempty"`
	Description     string       `json:"description,omitempty"`
	RegisterAs      string       `json:"registerAs,omitempty"`
}

// TransactionReceipt is sent when a transaction has been successfully mined
// For the big numbers, we pass a simple string as well as a full
// ethereum hex encoding version
type TransactionReceipt struct {
	ReplyCommon
	BlockHash            *common.Hash    `json:"blockHash"`
	BlockNumberStr       string          `json:"blockNumber"`
	BlockNumberHex       *hexutil.Big    `json:"blockNumberHex,omitempty"`
	ContractSwagger      string          `json:"openapi,omitempty"`
	ContractUI           string          `json:"apiexerciser,omitempty"`
	ContractAddress      *common.Address `json:"contractAddress,omitempty"`
	CumulativeGasUsedStr string          `json:"cumulativeGasUsed"`
	CumulativeGasUsedHex *hexutil.Big    `json:"cumulativeGasUsedHex,omitempty"`
	From                 *common.Address `json:"from"`
	GasUsedStr           string          `json:"gasUsed"`
	GasUsedHex           *hexutil.Big    `json:"gasUsedHex,omitempty"`
	NonceStr             string          `json:"nonce"`
	NonceHex             *hexutil.Uint64 `json:"nonceHex,omitempty"`
	StatusStr            string          `json:"status"`
	StatusHex            *hexutil.Big    `json:"statusHex,omitempty"`
	To                   *common.Address `json:"to"`
	TransactionHash      *common.Hash    `json:"transactionHash"`
	TransactionIndexStr  string          `json:"transactionIndex"`
	TransactionIndexHex  *hexutil.Uint   `json:"transactionIndexHex,omitempty"`
	RegisterAs           string          `json:"registerAs,omitempty"`
}

// ErrorReply is
type ErrorReply struct {
	ReplyCommon
	ErrorMessage     string `json:"errorMessage,omitempty"`
	OriginalMessage  string `json:"requestPayload,omitempty"`
	TXHash           string `json:"transactionHash,omitempty"`
	GapFillTxHash    string `json:"gapFillTxHash,omitempty"`
	GapFillSucceeded *bool  `json:"gapFillSucceeded,omitempty"`
}

// NewErrorReply is a helper to construct an error message
func NewErrorReply(err error, origMsg interface{}) *ErrorReply {
	var errMsg ErrorReply
	errMsg.Headers.MsgType = MsgTypeError
	if err != nil {
		errMsg.ErrorMessage = err.Error()
	}
	if reflect.TypeOf(origMsg).Kind() == reflect.Slice {
		errMsg.OriginalMessage = string(origMsg.([]byte))
	} else {
		origMsgBytes, _ := json.Marshal(origMsg)
		if origMsgBytes != nil {
			errMsg.OriginalMessage = string(origMsgBytes)
		}
	}
	return &errMsg
}
