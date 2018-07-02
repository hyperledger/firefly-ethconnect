// Copyright 2018 Kaleido, a ConsenSys business

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

	"github.com/spf13/cobra"
)

// AllOrNoneReqd util for checking Cobra parameters in PreRunE validators
func AllOrNoneReqd(cmd *cobra.Command, opts ...string) (err error) {
	var setFlags, unsetFlags []string
	for _, opt := range opts {
		if optVal, err := cmd.Flags().GetString(opt); err == nil {
			if optVal != "" {
				setFlags = append(setFlags, opt)
			} else {
				unsetFlags = append(unsetFlags, opt)
			}
		} else {
			return err
		}
	}
	if len(setFlags) != 0 && len(unsetFlags) != 0 {
		return fmt.Errorf("flag mismatch: '%s' set and '%s' unset", strings.Join(setFlags, ","), strings.Join(unsetFlags, ","))
	}
	return
}
