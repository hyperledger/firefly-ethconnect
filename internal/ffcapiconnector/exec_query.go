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

	"github.com/hyperledger/firefly-common/pkg/ffcapi"
	"github.com/hyperledger/firefly-ethconnect/internal/errors"
)

func (s *ffcServer) execQuery(ctx context.Context, payload []byte) (interface{}, ffcapi.ErrorReason, error) {

	// TODO - currently ethconnect returns parameters as a map, and this seems inconsistent.
	//        We should reconcile this in the FFCAPI implementation, while maintaining existing behavior for EthConnect default paths.
	return nil, "", errors.Errorf(errors.FFCRequestTypeNotImplemented, ffcapi.RequestTypeExecQuery)

}
