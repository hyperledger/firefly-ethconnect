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
	"fmt"
	"testing"

	"github.com/hyperledger/firefly-common/pkg/ffcapi"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const sampleGetNewBlockHashes = `{
	"ffcapi": {
		"version": "v1.0.0",
		"id": "904F177C-C790-4B01-BDF4-F2B4E52E607E",
		"type": "get_new_block_hashes"
	},
	"listenerId": "0x3039"
}`

func TestGetNewBlockHashesOK(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges",
		mock.MatchedBy(
			func(listenerID *ethbinding.HexBigInt) bool {
				return listenerID.String() == "0x3039"
			})).
		Return(nil).
		Run(func(args mock.Arguments) {
			*(args[1].(*[]string)) = []string{"0x12345", "0x23456"}
		})

	iRes, reason, err := s.getNewBlockHashes(ctx, []byte(sampleGetNewBlockHashes))
	assert.NoError(t, err)
	assert.Empty(t, reason)

	res := iRes.(*ffcapi.GetNewBlockHashesResponse)
	assert.Equal(t, []string{"0x12345", "0x23456"}, res.BlockHashes)

}

func TestGetNewBlockHashesFail(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.Anything).
		Return(fmt.Errorf("pop"))

	iRes, reason, err := s.getNewBlockHashes(ctx, []byte(sampleGetNewBlockHashes))
	assert.Regexp(t, "pop", err)
	assert.Empty(t, reason)
	assert.Nil(t, iRes)

}

func TestGetNewBlockHashesFailNotFound(t *testing.T) {

	s, mRPC := newTestFFCAPIServer()
	ctx := context.Background()

	mRPC.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterChanges", mock.Anything).
		Return(fmt.Errorf("filter not found"))

	iRes, reason, err := s.getNewBlockHashes(ctx, []byte(sampleGetNewBlockHashes))
	assert.Regexp(t, "filter not found", err)
	assert.Equal(t, ffcapi.ErrorReasonNotFound, reason)
	assert.Nil(t, iRes)

}

func TestGetNewBlockHashesBadPayload(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	ctx := context.Background()

	iRes, reason, err := s.getNewBlockHashes(ctx, []byte("!json"))
	assert.Regexp(t, "invalid", err)
	assert.Equal(t, ffcapi.ErrorReasonInvalidInputs, reason)
	assert.Nil(t, iRes)

}
