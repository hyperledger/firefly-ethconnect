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
	"fmt"

	"github.com/hyperledger/firefly-common/pkg/ffcapi"
	"github.com/hyperledger/firefly-common/pkg/fftypes"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
)

func (s *ffcServer) getGasPrice(ctx context.Context, payload []byte) (interface{}, ffcapi.ErrorReason, error) {

	var req ffcapi.GetGasPriceRequest
	err := json.Unmarshal(payload, &req)
	if err != nil {
		return nil, ffcapi.ErrorReasonInvalidInputs, err
	}

	// Note we use simple (pre London fork) gas fee approach.
	// See https://github.com/ethereum/pm/issues/328#issuecomment-853234014 for a bit of color
	var gasPrice ethbinding.HexBigInt
	err = s.rpc.CallContext(ctx, &gasPrice, "eth_gasPrice")
	if err != nil {
		return nil, "", err
	}

	return &ffcapi.GetGasPriceResponse{
		GasPrice: fftypes.JSONAnyPtr(fmt.Sprintf(`"%s"`, gasPrice.ToInt().Text(10))),
	}, "", nil

}
