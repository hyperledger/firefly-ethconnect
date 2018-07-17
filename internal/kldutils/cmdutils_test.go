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
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestAllOrNoneReqd(t *testing.T) {

	assert := assert.New(t)

	cmd := &cobra.Command{
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			return
		},
	}
	cmd.Flags().String("flag1", "", "Flag 1")
	cmd.Flags().String("flag2", "", "Flag 2")

	err := AllOrNoneReqd(cmd, "flag1", "flag2")
	assert.Nil(err)

	cmd.Flags().Set("flag1", "value1")

	err = AllOrNoneReqd(cmd, "flag1", "flag2")
	assert.Regexp("flag mismatch", err.Error())

	cmd.Flags().Set("flag2", "value2")

	err = AllOrNoneReqd(cmd, "flag1", "flag2")
	assert.Nil(err)

	err = AllOrNoneReqd(cmd, "random")
	assert.Regexp("flag accessed but not defined", err.Error())

}

func TestDefInt(t *testing.T) {

	assert := assert.New(t)

	os.Unsetenv("SOME_ENV_VAR")

	val := DefInt("SOME_ENV_VAR", 12345)
	assert.Equal(12345, val)

	os.Setenv("SOME_ENV_VAR", "not a number!")

	val = DefInt("SOME_ENV_VAR", 12345)
	assert.Equal(12345, val)

	os.Setenv("SOME_ENV_VAR", "54321")
	val = DefInt("SOME_ENV_VAR", 12345)
	assert.Equal(54321, val)

}
