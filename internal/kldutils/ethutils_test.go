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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStrToAddress(t *testing.T) {

	assert := assert.New(t)

	_, err := StrToAddress("missing", "")
	assert.Regexp("must be supplied", err.Error())

	_, err = StrToAddress("will not parse", "badness")
	assert.Regexp("is not a valid hex address", err.Error())

	addr, err := StrToAddress("good one with hex prefix", "0xd15aD5D4a0853585d655B30819C16bAAed412FFf")
	assert.Nil(err)
	assert.Equal("0xd15aD5D4a0853585d655B30819C16bAAed412FFf", addr.Hex())

	addr, err = StrToAddress("good one without prefix", "d15aD5D4a0853585d655B30819C16bAAed412FFf")
	assert.Nil(err)
	assert.Equal("0xd15aD5D4a0853585d655B30819C16bAAed412FFf", addr.Hex())

}
