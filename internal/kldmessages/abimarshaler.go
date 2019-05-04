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

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// UnmarshalJSON pass through the unmarshal (which abi.ABI implements)
func (abi *ABI) UnmarshalJSON(data []byte) error {
	return abi.ABI.UnmarshalJSON(data)
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

func marshalArgs(abiArgs []abi.Argument) []arg {
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
