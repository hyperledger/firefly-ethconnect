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
	"testing"

	"github.com/kaleido-io/ethconnect/internal/kldbind"

	"github.com/stretchr/testify/assert"
)

func TestTopicToValue(t *testing.T) {
	assert := assert.New(t)

	h := kldbind.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffcfc7")
	v := topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("int64")})
	assert.Equal("-12345", v)

	h = kldbind.HexToHash("0x000000000000000000000000000000000000000001d2d490d572353317a01f8d")
	v = topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("uint256")})
	assert.Equal("564363245346346345353453453", v)

	h = kldbind.HexToHash("0x0000000000000000000000003924d1d6423f88148a4fcc0417a33b27a61d595f")
	v = topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("address")})
	assert.Equal(kldbind.HexToAddress("0x3924d1D6423F88148A4fcc0417A33B27a61d595f"), v)

	h = kldbind.HexToHash("0xdc47fb175244491f21a29733a67d2e07647d59d2f36f2603d339299587182f19")
	v = topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("string")})
	assert.Equal("0xdc47fb175244491f21a29733a67d2e07647d59d2f36f2603d339299587182f19", v)

	h = kldbind.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000")
	v = topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("bool")})
	assert.Equal(false, v)

	h = kldbind.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	v = topicToValue(&h, &kldbind.ABIArgument{Type: kldbind.ABITypeKnown("bool")})
	assert.Equal(true, v)

}

func TestProcessLogEntryNillAndTooFewFields(t *testing.T) {
	assert := assert.New(t)

	lp := &logProcessor{
		event: &kldbind.ABIEvent{
			Anonymous: true,
			Inputs: []kldbind.ABIArgument{
				kldbind.ABIArgument{
					Name:    "one",
					Indexed: true,
				},
				kldbind.ABIArgument{
					Name:    "two",
					Indexed: true,
				},
			},
		},
	}
	err := lp.processLogEntry("ut", &logEntry{
		Topics: []*kldbind.Hash{nil},
	}, 2)

	assert.EqualError(err, "ut: Ran out of topics for indexed fields at field 1 of event ( indexed one,  indexed two)")
}

func TestProcessLogBadRLPData(t *testing.T) {
	assert := assert.New(t)

	lp := &logProcessor{
		event: &kldbind.ABIEvent{
			Anonymous: true,
			Inputs: []kldbind.ABIArgument{
				kldbind.ABIArgument{
					Name:    "one",
					Indexed: false,
				},
				kldbind.ABIArgument{
					Name:    "two",
					Indexed: false,
				},
			},
		},
	}
	err := lp.processLogEntry(t.Name(), &logEntry{
		Data: "0x00",
	}, 0)

	assert.Regexp("Failed to parse RLP data from event", err.Error())
}
