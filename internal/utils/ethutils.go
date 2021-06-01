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

package utils

import (
	"strings"

	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	"github.com/kaleido-io/ethconnect/internal/errors"
	"github.com/kaleido-io/ethconnect/internal/ethbind" 
)

// StrToAddress is a helper to parse eth addresses with useful errors
func StrToAddress(desc string, strAddr string) (addr ethbinding.Address, err error) {
	if strAddr == "" {
		err = errors.Errorf(errors.HelperStrToAddressRequiredField, desc)
		return
	}
	if !strings.HasPrefix(strAddr, "0x") {
		strAddr = "0x" + strAddr
	}
	if !ethbind.API.IsHexAddress(strAddr) {
		err = errors.Errorf(errors.HelperStrToAddressBadAddress, desc)
		return
	}
	addr = ethbind.API.HexToAddress(strAddr)
	return
}
