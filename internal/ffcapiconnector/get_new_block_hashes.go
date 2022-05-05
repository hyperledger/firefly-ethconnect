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
	"encoding/json"

	"github.com/hyperledger/firefly-common/pkg/ffcapi"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
)

func (s *ffcServer) getNewBlockHashes(ctx context.Context, payload []byte) (interface{}, ffcapi.ErrorReason, error) {

	var req ffcapi.GetNewBlockHashesRequest
	err := json.Unmarshal(payload, &req)
	if err != nil {
		return nil, ffcapi.ErrorReasonInvalidInputs, err
	}

	var listenerID ethbinding.HexBigInt
	listenerID.ToInt().SetString(req.ListenerID, 0 /* big.Int strips the 0x */)
	var blockHashes []string
	err = s.rpc.CallContext(ctx, &blockHashes, "eth_getFilterChanges", &listenerID)
	if err != nil {
		return nil, mapError(filterRPCMethods, err), err
	}

	return &ffcapi.GetNewBlockHashesResponse{
		BlockHashes: blockHashes,
	}, "", nil

}
