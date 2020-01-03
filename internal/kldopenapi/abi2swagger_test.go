// Copyright 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldopenapi

import (
	"encoding/json"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/stretchr/testify/assert"
)

const (
	erc20ABI           = "[{\"constant\":false,\"inputs\":[{\"name\":\"spender\",\"type\":\"address\"},{\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"approve\",\"outputs\":[{\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[{\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"name\":\"from\",\"type\":\"address\"},{\"name\":\"to\",\"type\":\"address\"},{\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"transferFrom\",\"outputs\":[{\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"name\":\"spender\",\"type\":\"address\"},{\"name\":\"addedValue\",\"type\":\"uint256\"}],\"name\":\"increaseAllowance\",\"outputs\":[{\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"name\":\"owner\",\"type\":\"address\"}],\"name\":\"balanceOf\",\"outputs\":[{\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"name\":\"spender\",\"type\":\"address\"},{\"name\":\"subtractedValue\",\"type\":\"uint256\"}],\"name\":\"decreaseAllowance\",\"outputs\":[{\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"name\":\"to\",\"type\":\"address\"},{\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"transfer\",\"outputs\":[{\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"name\":\"owner\",\"type\":\"address\"},{\"name\":\"spender\",\"type\":\"address\"}],\"name\":\"allowance\",\"outputs\":[{\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"name\":\"from\",\"type\":\"address\"},{\"indexed\":true,\"name\":\"to\",\"type\":\"address\"},{\"indexed\":false,\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Transfer\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"name\":\"spender\",\"type\":\"address\"},{\"indexed\":false,\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Approval\",\"type\":\"event\"}]"
	erc20DevDocs       = "{\"details\":\"Implementation of the basic standard token. https://eips.ethereum.org/EIPS/eip-20 Originally based on code by FirstBlood: https://github.com/Firstbloodio/token/blob/master/smart_contract/FirstBloodToken.sol * This implementation emits additional Approval events, allowing applications to reconstruct the allowance status for all accounts just by listening to said events. Note that this isn't required by the specification, and other compliant implementations may not do it.\",\"methods\":{\"allowance(address,address)\":{\"details\":\"Function to check the amount of tokens that an owner allowed to a spender.\",\"params\":{\"owner\":\"address The address which owns the funds.\",\"spender\":\"address The address which will spend the funds.\"},\"return\":\"A uint256 specifying the amount of tokens still available for the spender.\"},\"approve(address,uint256)\":{\"details\":\"Approve the passed address to spend the specified amount of tokens on behalf of msg.sender. Beware that changing an allowance with this method brings the risk that someone may use both the old and the new allowance by unfortunate transaction ordering. One possible solution to mitigate this race condition is to first reduce the spender's allowance to 0 and set the desired value afterwards: https://github.com/ethereum/EIPs/issues/20#issuecomment-263524729\",\"params\":{\"spender\":\"The address which will spend the funds.\",\"value\":\"The amount of tokens to be spent.\"}},\"balanceOf(address)\":{\"details\":\"Gets the balance of the specified address.\",\"params\":{\"owner\":\"The address to query the balance of.\"},\"return\":\"A uint256 representing the amount owned by the passed address.\"},\"decreaseAllowance(address,uint256)\":{\"details\":\"Decrease the amount of tokens that an owner allowed to a spender. approve should be called when _allowed[msg.sender][spender] == 0. To decrement allowed value is better to use this function to avoid 2 calls (and wait until the first transaction is mined) From MonolithDAO Token.sol Emits an Approval event.\",\"params\":{\"spender\":\"The address which will spend the funds.\",\"subtractedValue\":\"The amount of tokens to decrease the allowance by.\"}},\"increaseAllowance(address,uint256)\":{\"details\":\"Increase the amount of tokens that an owner allowed to a spender. approve should be called when _allowed[msg.sender][spender] == 0. To increment allowed value is better to use this function to avoid 2 calls (and wait until the first transaction is mined) From MonolithDAO Token.sol Emits an Approval event.\",\"params\":{\"addedValue\":\"The amount of tokens to increase the allowance by.\",\"spender\":\"The address which will spend the funds.\"}},\"totalSupply()\":{\"details\":\"Total number of tokens in existence.\"},\"transfer(address,uint256)\":{\"details\":\"Transfer token to a specified address.\",\"params\":{\"to\":\"The address to transfer to.\",\"value\":\"The amount to be transferred.\"}},\"transferFrom(address,address,uint256)\":{\"details\":\"Transfer tokens from one address to another. Note that while this function emits an Approval event, this is not required as per the specification, and other compliant implementations may not emit the event.\",\"params\":{\"from\":\"address The address which you want to send tokens from\",\"to\":\"address The address which you want to transfer to\",\"value\":\"uint256 the amount of tokens to be transferred\"}}},\"title\":\"Standard ERC20 token\"}"
	lotsOfTypesABI     = "[{\"constant\":false,\"inputs\":[{\"name\":\"param1\",\"type\":\"uint256\"},{\"name\":\"param2\",\"type\":\"uint256\"},{\"name\":\"param3\",\"type\":\"uint256\"},{\"name\":\"param4\",\"type\":\"uint256\"},{\"name\":\"param5\",\"type\":\"uint256\"},{\"name\":\"param6\",\"type\":\"bool\"}],\"name\":\"undocumentedWrites\",\"outputs\":[{\"name\":\"\",\"type\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\"},{\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"name\":\"param1\",\"type\":\"uint8\"},{\"name\":\"param2\",\"type\":\"bytes\"},{\"name\":\"param3\",\"type\":\"uint256[]\"},{\"name\":\"param4\",\"type\":\"bytes1[]\"},{\"name\":\"param5\",\"type\":\"bytes32\"},{\"name\":\"param6\",\"type\":\"bool[]\"},{\"name\":\"param7\",\"type\":\"address[]\"}],\"name\":\"echoTypes1\",\"outputs\":[{\"name\":\"retval1\",\"type\":\"uint8\"},{\"name\":\"retval2\",\"type\":\"bytes\"},{\"name\":\"retval3\",\"type\":\"uint256[]\"},{\"name\":\"retval4\",\"type\":\"bytes1[]\"},{\"name\":\"retval5\",\"type\":\"bytes32\"},{\"name\":\"retval6\",\"type\":\"bool[]\"},{\"name\":\"retval7\",\"type\":\"address[]\"}],\"payable\":false,\"stateMutability\":\"pure\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"name\":\"param1\",\"type\":\"string\"},{\"name\":\"param2\",\"type\":\"int256[]\"},{\"name\":\"param3\",\"type\":\"bool\"},{\"name\":\"param4\",\"type\":\"bytes1\"},{\"name\":\"param5\",\"type\":\"address\"},{\"name\":\"param6\",\"type\":\"bytes4\"},{\"name\":\"param7\",\"type\":\"uint256\"}],\"name\":\"echoTypes2\",\"outputs\":[{\"name\":\"retval1\",\"type\":\"string\"},{\"name\":\"retval2\",\"type\":\"int256[]\"},{\"name\":\"retval3\",\"type\":\"bool\"},{\"name\":\"retval4\",\"type\":\"bytes1\"},{\"name\":\"retval5\",\"type\":\"address\"},{\"name\":\"retval6\",\"type\":\"bytes4\"},{\"name\":\"retval7\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"pure\",\"type\":\"function\"}]"
	lotsOfTypesDevDocs = "{\"details\":\"Challenges the swagger generator to use lots of types\",\"methods\":{\"echoTypes1(uint8,bytes,uint256[],bytes1[],bytes32,bool[],address[])\":{\"details\":\"Echo back some types\",\"params\":{\"param1\":\"Parameter 1\",\"param2\":\"Parameter 2\",\"param3\":\"Parameter 3\",\"param4\":\"Parameter 4\",\"param5\":\"Parameter 5\",\"param6\":\"Parameter 6\",\"param7\":\"Parameter 7\"},\"return\":\"all of the individual input parameters\"},\"echoTypes2(string,int256[],bool,bytes1,address,bytes4,uint256)\":{\"details\":\"Echo back some more types\",\"params\":{\"param1\":\"Parameter 1\",\"param2\":\"Parameter 2\",\"param3\":\"Parameter 3\",\"param4\":\"Parameter 4\",\"param5\":\"Parameter 5\",\"param6\":\"Parameter 6\"},\"return\":\"all of the individual input parameters\"}},\"title\":\"LotsOfTypes\"}"
)

func TestABI2SwaggerERC20Generic(t *testing.T) {
	assert := assert.New(t)

	c := NewABI2Swagger("localhost:80", "/contracts", []string{"http"}, true)
	abi, err := abi.JSON(strings.NewReader(erc20ABI))
	assert.NoError(err)
	swagger := c.Gen4Factory("/erc20", "erc20", false, false, &abi, erc20DevDocs)

	swaggerBytes, err := json.MarshalIndent(&swagger, "", "  ")
	assert.NoError(err)
	t.Log(string(swaggerBytes))

	expectedJSON, _ := ioutil.ReadFile("../../test/erc20.swagger.json")
	assert.Equal(string(expectedJSON), string(swaggerBytes))
	return
}

func TestABI2SwaggerERC20GenericFactoryOnly(t *testing.T) {
	assert := assert.New(t)

	c := NewABI2Swagger("localhost:80", "/contracts", []string{"http"}, false)
	abi, err := abi.JSON(strings.NewReader(erc20ABI))
	assert.NoError(err)
	swagger := c.Gen4Factory("/erc20", "erc20", true, false, &abi, erc20DevDocs)

	assert.Equal(1, len(swagger.Paths.Paths))
	return
}

func TestABI2SwaggerLotsOfTypesInstance(t *testing.T) {
	assert := assert.New(t)

	c := NewABI2Swagger("localhost", "/contracts", nil, true)
	abi, err := abi.JSON(strings.NewReader(lotsOfTypesABI))
	assert.NoError(err)
	swagger := c.Gen4Instance("/0x0123456789abcdef0123456789abcdef0123456", "lotsOfTypes", &abi, lotsOfTypesDevDocs)

	swaggerBytes, err := json.MarshalIndent(&swagger, "", "  ")
	assert.NoError(err)
	t.Log(string(swaggerBytes))

	expectedJSON, _ := ioutil.ReadFile("../../test/lotsoftypes.swagger.json")
	assert.Equal(string(expectedJSON), string(swaggerBytes))
	return
}
