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
	"testing"

	"github.com/ethereum/go-ethereum/common"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestGetTransactionCount(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{}

	addr := common.HexToAddress("0xD50ce736021D9F7B0B2566a3D2FA7FA3136C003C")
	_, err := GetTransactionCount(&r, &addr, "latest")

	assert.Equal(nil, err)
	assert.Equal("eth_getTransactionCount", r.capturedMethod)
}
