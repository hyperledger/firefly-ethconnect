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

package kldeth

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
)

// OrionPrivacyGroup is the result of the priv_findPrivacyGroup call
type OrionPrivacyGroup struct {
	PrivacyGroupID string `json:"privacyGroupId"`
}

// GetOrionPrivacyGroup resolves privateFrom/privateFor into a privacyGroupID
func GetOrionPrivacyGroup(ctx context.Context, rpc RPCClient, addr *common.Address, privateFrom string, privateFor []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	allMembers := []string{privateFrom}
	allMembers = append(allMembers, privateFor...)
	params := map[string]interface{}{
		"addresses": allMembers,
	}
	// var privacyGroup OrionPrivacyGroup
	var privacyGroups []OrionPrivacyGroup
	var privacyGroup string
	if err := rpc.CallContext(ctx, &privacyGroups, "priv_findPrivacyGroup", allMembers); err != nil {
		return "", klderrors.Errorf(klderrors.RPCCallReturnedError, "priv_findPrivacyGroup", err)
	}
	if len(privacyGroups) == 0 {
		if err := rpc.CallContext(ctx, &privacyGroup, "priv_createPrivacyGroup", params); err != nil {
			return "", klderrors.Errorf(klderrors.RPCCallReturnedError, "priv_createPrivacyGroup", err)
		}
	} else {
		privacyGroup = privacyGroups[0].PrivacyGroupID
	}
	return privacyGroup, nil
}
