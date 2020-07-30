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

// ABIEvent is an event on the ABI
type ABIEvent = abi.Event

// ABIArguments is an array of arguments with helper functions
type ABIArguments = abi.Arguments

// ABIArgument is an argument in the Inputs or Outputs of an ABI
type ABIArgument = abi.Argument

// ABIType is a type
type ABIType = abi.Type

// ABIMethod is an method on the ABI
type ABIMethod = abi.Method

// ABIArgumentMarshaling is abi.ArgumentMarshaling
type ABIArgumentMarshaling = abi.ArgumentMarshaling

// ABI is a wrapper around the ethereum ABI implementation that includes
// marshal, as well as unmarshal
type ABI struct {
	abi.ABI
}

// NewABIEvent constructor for abi.Event
func NewABIEvent(name, rawName string, anonymous bool, inputs ABIArguments) *ABIEvent {
	abiEvent := abi.NewEvent(name, rawName, anonymous, inputs)
	return &abiEvent
}

// NewABIType constructor for abi.Type
func NewABIType(t string, internalType string, components []ABIArgumentMarshaling) (ABIType, error) {
	return abi.NewType(t, internalType, components)
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
