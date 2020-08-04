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
	"io/ioutil"
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

func TestABITypeKnown(t *testing.T) {
	assert := assert.New(t)
	assert.Equal("uint256", ABITypeKnown("uint256").String())
}

func TestABISignature(t *testing.T) {
	assert := assert.New(t)

	ev := abi.NewEvent("test", "test", true, ABIArguments{ABIArgument{
		Name: "arg1",
		Type: ABITypeKnown("uint256"),
	}})
	assert.Equal("test(uint256)", ABIEventSignature(&ev))

}

func TestRABIMarshalingToABIRuntime(t *testing.T) {
	assert := assert.New(t)

	var marshalableABI ABIMarshaling
	b, err := ioutil.ReadFile("../../test/abicoderv2_example.abi.json")
	assert.NoError(err)
	err = json.Unmarshal(b, &marshalableABI)
	assert.NoError(err)

	runtimeABI, err := ABIMarshalingToABIRuntime(marshalableABI)
	assert.NoError(err)
	assert.Equal(abi.TupleTy, runtimeABI.Methods["inOutType1"].Inputs[0].Type.T)
}

func TestRABIMarshalingToABIRuntimeSimpleStorage(t *testing.T) {
	assert := assert.New(t)

	var marshalableABI ABIMarshaling
	b, err := ioutil.ReadFile("../../test/simplestorage.abi.json")
	assert.NoError(err)
	err = json.Unmarshal(b, &marshalableABI)
	assert.NoError(err)

	runtimeABI, err := ABIMarshalingToABIRuntime(marshalableABI)
	assert.NoError(err)
	assert.Equal("x", runtimeABI.ABI.Methods["set"].Inputs[0].Name)
	assert.Equal(abi.UintTy, runtimeABI.ABI.Methods["set"].Inputs[0].Type.T)
}

func TestABIElementMarshalingToABIEvent(t *testing.T) {
	assert := assert.New(t)

	eventBytes := `{
    "components": [
      {
        "internalType": "string",
        "name": "str1",
        "type": "string"
      },
      {
        "internalType": "uint232",
        "name": "val1",
        "type": "uint232"
      }
    ],
    "internalType": "struct ExampleV2Coder.Type1",
    "name": "input1",
    "type": "tuple"
  }`

	var input1 ABIArgumentMarshaling
	json.Unmarshal([]byte(eventBytes), &input1)
	marshalable := &ABIElementMarshaling{
		Name:   "testevent",
		Inputs: []ABIArgumentMarshaling{input1},
	}

	event, err := ABIElementMarshalingToABIEvent(marshalable)
	assert.NoError(err)
	assert.Equal(abi.TupleTy, event.Inputs[0].Type.T)

}

func TestABIElementMarshalingToABIMethod(t *testing.T) {
	assert := assert.New(t)

	eventBytes := `{
    "components": [
      {
        "internalType": "string",
        "name": "str1",
        "type": "string"
      },
      {
        "internalType": "uint232",
        "name": "val1",
        "type": "uint232"
      }
    ],
    "internalType": "struct ExampleV2Coder.Type1",
    "name": "input1",
    "type": "tuple"
  }`

	var input1 ABIArgumentMarshaling
	json.Unmarshal([]byte(eventBytes), &input1)
	marshalable := &ABIElementMarshaling{
		Name:     "testmethod",
		Constant: true,
		Inputs:   []ABIArgumentMarshaling{input1},
		Outputs:  []ABIArgumentMarshaling{input1},
	}

	method, err := ABIElementMarshalingToABIMethod(marshalable)
	assert.NoError(err)
	assert.Equal(abi.TupleTy, method.Inputs[0].Type.T)
	assert.Equal(abi.TupleTy, method.Outputs[0].Type.T)
	assert.Equal(true, method.IsConstant())

}

func TestABIElementMarshalingToABIMethodFail(t *testing.T) {
	assert := assert.New(t)

	marshalable := &ABIElementMarshaling{
		Name:     "testmethod",
		Constant: true,
		Inputs:   []ABIArgumentMarshaling{{Name: "test", Type: "badness"}},
	}

	_, err := ABIElementMarshalingToABIMethod(marshalable)
	assert.EqualError(err, "unsupported arg type: badness")

}
