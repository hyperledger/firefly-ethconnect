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
	"fmt"
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
	_, err := GetTransactionCount(context.Background(), &r, &addr, "latest")

	assert.Equal(nil, err)
	assert.Equal("eth_getTransactionCount", r.capturedMethod)
}

func TestGetTransactionCountErr(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{
		mockError: fmt.Errorf("pop"),
	}

	addr := common.HexToAddress("0xD50ce736021D9F7B0B2566a3D2FA7FA3136C003C")
	_, err := GetTransactionCount(context.Background(), &r, &addr, "latest")

	assert.EqualError(err, "eth_getTransactionCount returned: pop")
}

func TestGetOrionPrivateTransactionCount(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{}

	addr := common.HexToAddress("0xD50ce736021D9F7B0B2566a3D2FA7FA3136C003C")
	_, err := GetOrionTXCount(context.Background(), &r, &addr, "negmDcN2P4ODpqn/6WkJ02zT/0w0bjhGpkZ8UP6vARk=")

	assert.Equal(nil, err)
	assert.Equal("priv_getTransactionCount", r.capturedMethod)
}

func TestGetOrionPrivateTransactionCountErr(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{
		mockError: fmt.Errorf("pop"),
	}

	addr := common.HexToAddress("0xD50ce736021D9F7B0B2566a3D2FA7FA3136C003C")
	_, err := GetOrionTXCount(context.Background(), &r, &addr, "negmDcN2P4ODpqn/6WkJ02zT/0w0bjhGpkZ8UP6vARk=")

	assert.EqualError(err, "priv_getTransactionCount for privacy group 'negmDcN2P4ODpqn/6WkJ02zT/0w0bjhGpkZ8UP6vARk=' returned: pop")
}
