// Copyright 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldevents

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/kaleido-io/ethconnect/internal/kldbind"

	"github.com/stretchr/testify/assert"
)

func TestTopicToValue(t *testing.T) {
	assert := assert.New(t)

	h := kldbind.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffcfc7")
	v, err := topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("int64")})
	assert.NoError(err)
	assert.Equal(big.NewInt(-12345), v)

	h = kldbind.HexToHash("0x000000000000000000000000000000000000000001d2d490d572353317a01f8d")
	v, err = topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("uint256")})
	assert.NoError(err)
	assert.Equal(math.MustParseBig256("564363245346346345353453453"), v)

	h = kldbind.HexToHash("0x0000000000000000000000003924d1d6423f88148a4fcc0417a33b27a61d595f")
	v, err = topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("address")})
	assert.NoError(err)
	assert.Equal(kldbind.HexToAddress("0x3924d1D6423F88148A4fcc0417A33B27a61d595f"), v)

	h = kldbind.HexToHash("0xdc47fb175244491f21a29733a67d2e07647d59d2f36f2603d339299587182f19")
	v, err = topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("string")})
	assert.NoError(err)
	assert.Nil(v)

	h = kldbind.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000")
	v, err = topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("bool")})
	assert.NoError(err)
	assert.Equal(false, v)

	h = kldbind.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	v, err = topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("bool")})
	assert.NoError(err)
	assert.Equal(true, v)

}
