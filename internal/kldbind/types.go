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
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// This module provides some basic types proxied through from ethereum, to avoid
// ethereum imports throughout the codebase

// Address models and serializes a 20 byte ethereum address
type Address = common.Address

// Hash models and serializes a 32 byte ethereum hash
type Hash = common.Hash

// HexBigInt models and serializes big.Int
type HexBigInt = hexutil.Big

// HexUint64 models and serializes uint64
type HexUint64 = hexutil.Uint64

// HexUint models and serializes uint
type HexUint = hexutil.Uint

// ABIArguments is an array of arguments with helper functions
type ABIArguments = abi.Arguments

// ABIArgument is an argument in the Inputs or Outputs of an ABI
type ABIArgument = abi.Argument

// ABIType is a type
type ABIType = abi.Type

// ABIMethod is an method on the ABI
type ABIMethod = abi.Method

// ABIEvent is an event on the ABI
type ABIEvent = abi.Event

// ABIArgumentMarshaling is abi.ArgumentMarshaling
type ABIArgumentMarshaling struct {
	Name         string                  `json:"name"`
	Type         string                  `json:"type"`
	InternalType string                  `json:"internalType,omitempty"`
	Components   []ABIArgumentMarshaling `json:"components,omitempty"`
	Indexed      bool                    `json:"indexed,omitempty"`
}

// ABIElementMarshaling is the serialized representation of a method or event in an ABI
type ABIElementMarshaling struct {
	Type            string                  `json:"type,omitempty"`
	Name            string                  `json:"name,omitempty"`
	Payable         bool                    `json:"payable,omitempty"`
	Constant        bool                    `json:"constant,omitempty"`
	Anonymous       bool                    `json:"anonymous,omitempty"`
	StateMutability string                  `json:"stateMutability,omitempty"`
	Inputs          []ABIArgumentMarshaling `json:"inputs"`
	Outputs         []ABIArgumentMarshaling `json:"outputs"`
}

// ABIMarshaling is the JSON array representation of an ABI
type ABIMarshaling []ABIElementMarshaling

// RuntimeABI is the ethereum implementation of an ABI. It can be unmarshalled from an ABI JSON,
// but does not support marshalling.
type RuntimeABI struct {
	abi.ABI
}

// Header is a type for ethereum block Header representation
type Header = types.Header

const (
	// IntTy - type
	IntTy = abi.IntTy
	// UintTy - type
	UintTy = abi.UintTy
	// BoolTy - type
	BoolTy = abi.BoolTy
	// StringTy - type
	StringTy = abi.StringTy
	// BytesTy - type
	BytesTy = abi.BytesTy
	// FixedBytesTy - type
	FixedBytesTy = abi.FixedBytesTy
	// AddressTy - type
	AddressTy = abi.AddressTy
	// SliceTy - type
	SliceTy = abi.SliceTy
	// ArrayTy - type
	ArrayTy = abi.ArrayTy
)
