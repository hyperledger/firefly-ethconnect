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
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/assert"
)

func TestHexToAddress(t *testing.T) {
	assert := assert.New(t)
	assert.Equal("0x0123456789abcDEF0123456789abCDef01234567", HexToAddress("0x0123456789abcDEF0123456789abCDef01234567").String())
}

func TestBytesToAddress(t *testing.T) {
	assert := assert.New(t)
	b, _ := HexDecode("0x0123456789abcDEF0123456789abCDef01234567")
	assert.Equal("0x0123456789abcDEF0123456789abCDef01234567", BytesToAddress(b).String())
}

func TestHexToHash(t *testing.T) {
	assert := assert.New(t)
	assert.Equal("0x49d0d5ec93185ca4cd24efff28fdeef97bd06fe7a26f771e645892433184af13", HexToHash("0x49d0d5ec93185ca4cd24efff28fdeef97bd06fe7a26f771e645892433184af13").String())
}

func TestABITypeFor(t *testing.T) {
	assert := assert.New(t)
	abiType, _ := ABITypeFor("uint256")
	assert.Equal("uint256", abiType.String())
}

func TestABIMarshalUnMarshal(t *testing.T) {
	assert := assert.New(t)

	tUint256, _ := abi.NewType("uint256", "", []abi.ArgumentMarshaling{})
	a1 := ABI{
		ABI: abi.ABI{
			Constructor: abi.Method{
				Inputs: abi.Arguments{
					abi.Argument{Name: "carg1", Type: tUint256, Indexed: true},
				},
			},
			Methods: map[string]abi.Method{
				"method1": abi.Method{
					Name:    "method1",
					RawName: "method1",
					Const:   true,
					Inputs: abi.Arguments{
						abi.Argument{Name: "marg1", Type: tUint256, Indexed: true},
					},
					Outputs: abi.Arguments{
						abi.Argument{Name: "ret1", Type: tUint256, Indexed: true},
					},
				},
			},
			Events: map[string]abi.Event{
				"event1": abi.Event{
					Name:      "event1",
					RawName:   "event1",
					Anonymous: true,
					Inputs: abi.Arguments{
						abi.Argument{Name: "earg1", Type: tUint256, Indexed: true},
					},
				},
			},
		},
	}

	jsonBytes, err := json.Marshal(&a1)
	assert.NoError(err)

	var a2 ABI
	err = json.Unmarshal(jsonBytes, &a2)
	assert.NoError(err)

	t.Log(string(jsonBytes))

	assert.Equal(a1, a2)
}

func TestABIEventMarshalUnMarshal(t *testing.T) {
	assert := assert.New(t)

	me := MarshalledABIEvent{
		E: ABIEvent{
			Name:      "event1",
			Anonymous: true,
			Inputs: abi.Arguments{
				abi.Argument{Name: "earg1", Type: ABITypeKnown("uint256"), Indexed: true},
			},
		},
	}

	jsonBytes, err := json.Marshal(&me)
	assert.NoError(err)

	var me2 MarshalledABIEvent
	err = json.Unmarshal(jsonBytes, &me2)
	assert.NoError(err)

	t.Log(string(jsonBytes))

	assert.Equal(me, me2)

	badType := "{\"inputs\": [{\"type\":\"badness\"}]}"
	err = json.Unmarshal([]byte(badType), &me)
	assert.EqualError(err, "unsupported arg type: badness")

	badStuct := "{\"inputs\": false}"
	err = json.Unmarshal([]byte(badStuct), &me)
	assert.Regexp("cannot unmarshal", err.Error())
}
