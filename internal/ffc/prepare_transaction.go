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

package ffc

import (
	"context"
	"encoding/json"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/eth"
	"github.com/hyperledger/firefly-ethconnect/internal/messages"
	"github.com/hyperledger/firefly-transaction-manager/pkg/ffcapi"
	"github.com/hyperledger/firefly/pkg/fftypes"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	log "github.com/sirupsen/logrus"
)

func (s *ffcServer) prepareTransaction(ctx context.Context, payload []byte) (interface{}, ffcapi.ErrorReason, error) {

	var req ffcapi.PrepareTransactionRequest
	err := json.Unmarshal(payload, &req)
	if err != nil {
		return nil, ffcapi.ErrorReasonInvalidInputs, err
	}

	// Parse the input over to the standard EthConnect format
	sendTx, err := s.mapFFCAPIToEth(&req)
	if err != nil {
		return nil, ffcapi.ErrorReasonInvalidInputs, err
	}
	tx, err := eth.NewSendTxn(sendTx, nil)
	if err != nil {
		return nil, ffcapi.ErrorReasonInvalidInputs, err
	}

	gas := hexutil.Uint64(req.Gas.Int64())
	var data ethbinding.HexBytes
	if uint64(gas) == 0 {
		// If a value for gas has not been supplied, do a gas estimate
		var reverted bool
		data, gas, reverted, err = tx.Estimate(ctx, s.rpc, s.gasEstimationFactor)
		if err != nil {
			var reason ffcapi.ErrorReason
			if reverted {
				reason = ffcapi.ErrorReasonTransactionReverted
			}
			return nil, reason, err
		}
	} else {
		// Otherwise just serialize the data
		data = tx.EthTX.Data()
	}
	txHash := tx.EthTX.Hash().String()
	log.Infof("Prepared transaction %s method=%s datalen=%d gas=%d", txHash, sendTx.Method.Name, len(data), gas)

	return &ffcapi.PrepareTransactionResponse{
		Gas:             fftypes.NewFFBigInt(int64(gas)),
		TransactionHash: txHash,
		TransactionData: data.String(),
	}, "", nil

}

func (s *ffcServer) mapFFCAPIToEth(req *ffcapi.PrepareTransactionRequest) (*messages.SendTransaction, error) {

	// Parse the method ABI
	var ethMethod *ethbinding.ABIElementMarshaling
	err := json.Unmarshal([]byte(req.Method), &ethMethod)
	if err != nil {
		return nil, errors.Errorf(errors.FFCUnmarshalABIFail, err)
	}

	// Parse the params to go interface type, as handled by ethconnect standard SendTransaction handling
	ethParams := make([]interface{}, len(req.Params))
	for i, p := range req.Params {
		err := json.Unmarshal([]byte(p), &ethParams[i])
		if err != nil {
			return nil, errors.Errorf(errors.FFCUnmarshalParamFail, i, err)
		}
	}

	// First we need to encode the inputs
	st := &messages.SendTransaction{
		To:     req.To,
		Method: ethMethod,
		TransactionCommon: messages.TransactionCommon{
			From:       req.From,
			Parameters: ethParams,
		},
	}
	if req.Nonce != nil {
		st.Nonce = json.Number(req.Nonce.Int().String())
	}
	if req.Value != nil {
		st.Value = json.Number(req.Value.Int().String())
	}

	return st, nil
}
