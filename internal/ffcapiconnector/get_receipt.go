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
	"github.com/hyperledger/firefly-common/pkg/fftypes"
	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/eth"
)

func (s *ffcServer) getReceipt(ctx context.Context, payload []byte) (interface{}, ffcapi.ErrorReason, error) {

	var req ffcapi.GetReceiptRequest
	err := json.Unmarshal(payload, &req)
	if err != nil {
		return nil, ffcapi.ErrorReasonInvalidInputs, err
	}

	// Get the receipt in the back-end JSON/RPC format
	var ethReceipt eth.TxnReceipt
	err = s.rpc.CallContext(ctx, &ethReceipt, "eth_getTransactionReceipt", req.TransactionHash)
	if err != nil {
		return nil, "", err
	}
	isMined := ethReceipt.BlockHash != nil && ethReceipt.BlockNumber != nil && ethReceipt.BlockNumber.ToInt().Uint64() > 0
	if !isMined {
		return nil, ffcapi.ErrorReasonNotFound, errors.Errorf(errors.FFCReceiptNotAvailable, req.TransactionHash)
	}
	isSuccess := (ethReceipt.Status != nil && ethReceipt.Status.ToInt().Int64() > 0)

	fullReceipt, _ := json.Marshal(&ethReceipt)

	var txIndex int64
	if ethReceipt.TransactionIndex != nil {
		txIndex = int64(*ethReceipt.TransactionIndex)
	}
	return &ffcapi.GetReceiptResponse{
		BlockNumber:      (*fftypes.FFBigInt)(ethReceipt.BlockNumber.ToInt()),
		TransactionIndex: fftypes.NewFFBigInt(txIndex),
		BlockHash:        ethReceipt.BlockHash.String(),
		Success:          isSuccess,
		ExtraInfo:        *fftypes.JSONAnyPtrBytes(fullReceipt),
	}, "", nil

}
