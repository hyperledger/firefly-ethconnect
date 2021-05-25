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

package kldbind

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/compiler"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
)

// This module provides access to some utils from the common package, with
// type mapping

// HexDecode decodes an 0x prefixed hex string
func HexDecode(hex string) ([]byte, error) {
	return hexutil.Decode(hex)
}

// HexToAddress convert hex to an address
func HexToAddress(hex string) Address {
	var addr Address
	addr = common.HexToAddress(hex)
	return addr
}

// BytesToAddress converts bytes to address
func BytesToAddress(b []byte) Address {
	return common.BytesToAddress(b)
}

func IsHexAddress(s string) bool {
	return common.IsHexAddress(s)
}

// HexToHash convert hex to an address
func HexToHash(hex string) Hash {
	var hash Hash
	hash = common.HexToHash(hex)
	return hash
}

func EncodeBig(bigint *big.Int) string {
	return hexutil.EncodeBig(bigint)
}

// FromHex returns the bytes represented by the hexadecimal string s
func FromHex(hex string) []byte {
	return common.FromHex(hex)
}

// ToHex returns the hex representation of b, prefixed with '0x'. For empty slices, the return value is "0x0"
func ToHex(b []byte) string {
	return common.ToHex(b)
}

// ABITypeFor gives you a type for a string
func ABITypeFor(typeName string) (ABIType, error) {
	var t ABIType
	t, err := abi.NewType(typeName, "", []abi.ArgumentMarshaling{})
	return t, err
}

// ABITypeKnown gives you a type for a string you are sure is known
func ABITypeKnown(typeName string) ABIType {
	var t ABIType
	t, _ = abi.NewType(typeName, "", []abi.ArgumentMarshaling{})
	return t
}

// NewType creates a new reflection type of abi type given in t
func NewType(typeName string, internalType string) (typ abi.Type, err error) {
	return abi.NewType(typeName, internalType, []abi.ArgumentMarshaling{})
}

// ABIEventSignature returns a signature for an ABI event
func ABIEventSignature(event *ABIEvent) string {
	typeStrings := make([]string, len(event.Inputs))
	for i, input := range event.Inputs {
		typeStrings[i] = input.Type.String()
	}
	return fmt.Sprintf("%v(%v)", event.RawName, strings.Join(typeStrings, ","))
}

// ABIMarshalingToABIRuntime takes a serialized form ABI and converts it into RuntimeABI
// This is not performance optimized, so the RuntimeABI once generated should be used
// for runtime processing
func ABIMarshalingToABIRuntime(marshalable ABIMarshaling) (*RuntimeABI, error) {
	var runtime RuntimeABI
	b, _ := json.Marshal(&marshalable)
	err := json.Unmarshal(b, &runtime)
	return &runtime, err
}

// ABIArgumentsMarshalingToABIArguments converts ABI serialized reprsentations of arguments
// to the processed type information
func ABIArgumentsMarshalingToABIArguments(marshalable []ABIArgumentMarshaling) (ABIArguments, error) {
	arguments := make(ABIArguments, len(marshalable))
	var err error
	for i, mArg := range marshalable {
		var components []abi.ArgumentMarshaling
		if mArg.Components != nil {
			b, _ := json.Marshal(&mArg.Components)
			json.Unmarshal(b, &components)
		}
		arguments[i].Type, err = abi.NewType(mArg.Type, mArg.InternalType, components)
		if err != nil {
			return nil, err
		}
		arguments[i].Name = mArg.Name
		arguments[i].Indexed = mArg.Indexed
	}
	return arguments, nil
}

// ABIElementMarshalingToABIEvent converts a de-serialized event with full type information,
// per the original ABI, into a runtime ABIEvent with a processed type
func ABIElementMarshalingToABIEvent(marshalable *ABIElementMarshaling) (event *ABIEvent, err error) {
	inputs, err := ABIArgumentsMarshalingToABIArguments(marshalable.Inputs)
	if err == nil {
		nEvent := abi.NewEvent(marshalable.Name, marshalable.Name, marshalable.Anonymous, inputs)
		event = &nEvent
	}
	return
}

// ABIElementMarshalingToABIMethod converts a de-serialized method with full type information,
// per the original ABI, into a runtime ABIEvent with a processed type
func ABIElementMarshalingToABIMethod(m *ABIElementMarshaling) (method *ABIMethod, err error) {
	var inputs, outputs ABIArguments
	inputs, err = ABIArgumentsMarshalingToABIArguments(m.Inputs)
	if err == nil {
		outputs, err = ABIArgumentsMarshalingToABIArguments(m.Outputs)
		if err == nil {
			nMethod := abi.NewMethod(m.Name, m.Name, abi.Function, m.StateMutability, m.Constant, m.Payable, inputs, outputs)
			method = &nMethod
		}
	}
	return
}

// JSON returns a parsed ABI interface and error if it failed
func JSON(reader io.Reader) (abi.ABI, error) {
	return abi.JSON(reader)
}

func SolidityVersion(solc string) (*Solidity, error) {
	return compiler.SolidityVersion(solc)
}

func ParseCombinedJSON(combinedJSON []byte, source string, languageVersion string, compilerVersion string, compilerOptions string) (map[string]*Contract, error) {
	return compiler.ParseCombinedJSON(combinedJSON, source, languageVersion, compilerVersion, compilerOptions)
}

func Dial(rawurl string) (*Client, error) {
	return rpc.Dial(rawurl)
}

func NewMethod(name string, rawName string, funType ABIFunctionType, mutability string, isConst, isPayable bool, inputs ABIArguments, outputs ABIArguments) ABIMethod {
	return abi.NewMethod(name, rawName, funType, mutability, isConst, isPayable, inputs, outputs)
}

func NewTransaction(nonce uint64, to common.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *Transaction {
	return types.NewTransaction(nonce, to, amount, gasLimit, gasPrice, data)
}

func NewContractCreation(nonce uint64, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *Transaction {
	return types.NewContractCreation(nonce, amount, gasLimit, gasPrice, data)
}

func NewEIP155Signer(chainId *big.Int) EIP155Signer {
	return types.NewEIP155Signer(chainId)
}

func ParseBig256(s string) (*big.Int, bool) {
	return math.ParseBig256(s)
}

func S256(x *big.Int) *big.Int {
	return math.S256(x)
}

func GenerateKey() (*ecdsa.PrivateKey, error) {
	return crypto.GenerateKey()
}

func PubkeyToAddress(p ecdsa.PublicKey) Address {
	return crypto.PubkeyToAddress(p)
}

// FromECDSA exports a private key into a binary dump
func FromECDSA(priv *ecdsa.PrivateKey) []byte {
	return crypto.FromECDSA(priv)
}

// HexToECDSA parses a secp256k1 private key
func HexToECDSA(hexkey string) (*ecdsa.PrivateKey, error) {
	return crypto.HexToECDSA(hexkey)
}

// NewStream creates a new decoding stream reading from r
func NewStream(r io.Reader, inputLimit uint64) *rlp.Stream {
	return rlp.NewStream(r, inputLimit)
}

// SignTx signs the transaction using the given signer and private key
func SignTx(tx *types.Transaction, s types.Signer, prv *ecdsa.PrivateKey) (*types.Transaction, error) {
	return types.SignTx(tx, s, prv)
}
