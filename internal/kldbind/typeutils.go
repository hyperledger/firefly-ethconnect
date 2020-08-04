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
	"fmt"
	"strings"

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
