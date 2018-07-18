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

	"github.com/stretchr/testify/assert"
)

func TestAllOrNoneReqd(t *testing.T) {

	assert := assert.New(t)

	assert.Equal(true, AllOrNoneReqd("", ""))
	assert.Equal(false, AllOrNoneReqd("flag1", ""))
	assert.Equal(false, AllOrNoneReqd("", "flag2"))
	assert.Equal(true, AllOrNoneReqd("flag1", "flag2"))

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
