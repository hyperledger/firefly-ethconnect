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

package kldutils

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// StrToAddress is a helper to parse eth addresses with useful errors
func StrToAddress(desc string, strAddr string) (addr common.Address, err error) {
	if strAddr == "" {
		err = fmt.Errorf("'%s' must be supplied", desc)
		return
	}
	if !strings.HasPrefix(strAddr, "0x") {
		strAddr = "0x" + strAddr
	}
	if !common.IsHexAddress(strAddr) {
		err = fmt.Errorf("Supplied value for '%s' is not a valid hex address", desc)
		return
	}
	addr = common.HexToAddress(strAddr)
	return
}
