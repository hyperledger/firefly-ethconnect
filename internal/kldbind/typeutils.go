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
	"encoding/json"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
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

// HexToHash convert hex to an address
func HexToHash(hex string) Hash {
	var hash Hash
	hash = common.HexToHash(hex)
	return hash
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

type arg struct {
	Name    string `json:"name,omitempty"`
	Type    string `json:"type,omitempty"`
	Indexed bool   `json:"indexed"`
}

type field struct {
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	Constant  bool   `json:"constant,omitempty"`
	Anonymous bool   `json:"anonymous,omitempty"`
	Inputs    []arg
	Outputs   []arg
}

// UnmarshalJSON pass through the unmarshal (which abi.ABI implements)
func (abi *ABI) UnmarshalJSON(data []byte) error {
	return abi.ABI.UnmarshalJSON(data)
}

// MarshalJSON implements the reverse of UnmarshalJSON
func (abi *ABI) MarshalJSON() ([]byte, error) {
	var fields []field
	fields = append(fields, field{
		Type:   "constructor",
		Inputs: marshalArgs(abi.Constructor.Inputs),
	})
	for name, method := range abi.Methods {
		fields = append(fields, field{
			Type:     "function",
			Name:     name,
			Constant: method.Const,
			Inputs:   marshalArgs(method.Inputs),
			Outputs:  marshalArgs(method.Outputs),
		})
	}
	for name, event := range abi.Events {
		fields = append(fields, field{
			Type:      "event",
			Name:      name,
			Anonymous: event.Anonymous,
			Inputs:    marshalArgs(event.Inputs),
		})
	}
	return json.Marshal(&fields)
}

// MarshalledABIEvent needed because events can't be marshalled correctly either with abi.Event
type MarshalledABIEvent struct {
	E ABIEvent
}

// UnmarshalJSON converts to the inner structure
func (e *MarshalledABIEvent) UnmarshalJSON(data []byte) error {
	var field field
	if err := json.Unmarshal(data, &field); err != nil {
		return err
	}
	inputs, err := unmarshalArgs(field.Inputs)
	if err != nil {
		return err
	}
	e.E = ABIEvent{
		Name:      field.Name,
		Anonymous: field.Anonymous,
		Inputs:    inputs,
	}
	return nil
}

// MarshalJSON converts from the inner structure
func (e *MarshalledABIEvent) MarshalJSON() ([]byte, error) {
	field := field{
		Name:      e.E.Name,
		Anonymous: e.E.Anonymous,
		Inputs:    marshalArgs(e.E.Inputs),
	}
	return json.Marshal(&field)
}

func marshalArgs(abiArgs []ABIArgument) []arg {
	args := make([]arg, 0, len(abiArgs))
	for _, abiArg := range abiArgs {
		args = append(args, arg{
			Name:    abiArg.Name,
			Type:    abiArg.Type.String(),
			Indexed: abiArg.Indexed,
		})
	}
	return args
}

func unmarshalArgs(args []arg) ([]ABIArgument, error) {
	abiArgs := make([]ABIArgument, 0, len(args))
	for _, arg := range args {
		t, err := ABITypeFor(arg.Type)
		if err != nil {
			return nil, err
		}
		abiArgs = append(abiArgs, ABIArgument{
			Name:    arg.Name,
			Type:    t,
			Indexed: arg.Indexed,
		})
	}
	return abiArgs, nil
}
