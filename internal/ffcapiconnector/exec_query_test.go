// Copyright © 2022 Kaleido, Inc.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ffcapiconnector

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

const sampleExecQuery = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "exec_query"
	},
	"from": "0xb480F96c0a3d6E9e9a263e4665a39bFa6c4d01E8",
	"to": "0xe1a078b9e2b145d0a7387f09277c6ae1d9470771",
	"nonce": "222",
	"method": {
		"inputs": [],
		"name":"do",
		"outputs":[],
		"stateMutability":"nonpayable",
		"type":"function"
	},
	"method": {
		"inputs": [
			{
				"internalType":" uint256",
				"name": "x",
				"type": "uint256"
			}
		],
		"name":"set",
		"outputs":[
			{
				"internalType":" uint256",
				"name": "y",
				"type": "uint256"
			}
		],
		"stateMutability":"nonpayable",
		"type":"function"
	},
	"params": [ 4276993775 ]
}`

func TestExecQueryNotSupported(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()

	iRes, reason, err := s.execQuery(ctx, []byte(sampleExecQuery))
	assert.Regexp(t, "FFEC100217", err)
	assert.Empty(t, reason)
	assert.Nil(t, iRes)

}
