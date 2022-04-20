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

package events

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/hyperledger/firefly-ethconnect/internal/ethbind"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	"github.com/stretchr/testify/assert"
)

const sampleEventLogAllIndexedNoData = `{
  "address": "0x19e75d0d337e17835dc5246f007a1fb17f0bac89",
  "blockHash": "0xb6d8a38a89ac35a04ee6ebd5789a4a805dfa26c1b753c311db523ec9bf204384",
  "blockNumber": "0x74082",
  "data": "0x",
  "logIndex": 1,
  "removed": false,
  "topics": ["0x35d3551f6fc757e3146f18d79fbbaf97d788f77b23b07f25f5a80621072d5c70", "0x51b201b016025d42c9a0718b75aacc12b1e9c7f16e4bd2c6618aa944ca399156", "0x00000000000000000000000000000000000000000000000000000000000003e8"],
  "transactionHash": "0x23307094299f08a1041de9f1e7ecb67197a5a3c11ce5be775a8147de266b7524",
  "transactionIndex": "0x0"
}`

const sampleEventABIAllIndexedNoData = `
{
  "anonymous": false,
  "inputs": [
    {
      "indexed": true,
      "internalType": "string",
      "name": "data1",
      "type": "string"
    },
    {
      "indexed": true,
      "internalType": "uint256",
      "name": "data2",
      "type": "uint256"
    }
  ],
  "name": "SampleEvent",
  "type": "event"
}
`

func TestTopicToValue(t *testing.T) {
	assert := assert.New(t)

	h := ethbind.API.HexToHash("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffcfc7")
	v := topicToValue(&h, &ethbinding.ABIArgument{Type: ethbind.API.ABITypeKnown("int64")})
	assert.Equal("-12345", v)

	h = ethbind.API.HexToHash("0x000000000000000000000000000000000000000001d2d490d572353317a01f8d")
	v = topicToValue(&h, &ethbinding.ABIArgument{Type: ethbind.API.ABITypeKnown("uint256")})
	assert.Equal("564363245346346345353453453", v)

	h = ethbind.API.HexToHash("0x0000000000000000000000003924d1d6423f88148a4fcc0417a33b27a61d595f")
	v = topicToValue(&h, &ethbinding.ABIArgument{Type: ethbind.API.ABITypeKnown("address")})
	assert.Equal(ethbind.API.HexToAddress("0x3924d1D6423F88148A4fcc0417A33B27a61d595f"), v)

	h = ethbind.API.HexToHash("0xdc47fb175244491f21a29733a67d2e07647d59d2f36f2603d339299587182f19")
	v = topicToValue(&h, &ethbinding.ABIArgument{Type: ethbind.API.ABITypeKnown("string")})
	assert.Equal("0xdc47fb175244491f21a29733a67d2e07647d59d2f36f2603d339299587182f19", v)

	h = ethbind.API.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000")
	v = topicToValue(&h, &ethbinding.ABIArgument{Type: ethbind.API.ABITypeKnown("bool")})
	assert.Equal(false, v)

	h = ethbind.API.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	v = topicToValue(&h, &ethbinding.ABIArgument{Type: ethbind.API.ABITypeKnown("bool")})
	assert.Equal(true, v)

}

func TestProcessLogEntryNillAndTooFewFields(t *testing.T) {
	assert := assert.New(t)

	spec := &StreamInfo{
		Timestamps: false,
	}
	stream := &eventStream{
		spec: spec,
	}

	eventABI := `{
    "name": "testEvent",
    "anonymous": true,
    "inputs": [
      {"name": "one", "type": "uint256", "indexed": true},
      {"name": "two", "type": "uint256", "indexed": true}
    ]
  }`
	var marshaling ethbinding.ABIElementMarshaling
	json.Unmarshal([]byte(eventABI), &marshaling)
	event, err := ethbind.API.ABIElementMarshalingToABIEvent(&marshaling)
	assert.NoError(err)

	lp := &logProcessor{
		event:  event,
		stream: stream,
	}
	err = lp.processLogEntry("ut", &logEntry{
		Topics: []*ethbinding.Hash{nil},
	}, 2)

	assert.Regexp("ut: Ran out of topics for indexed fields at field 1 of testEvent\\(uint256,uint256\\)", err)
}

func TestProcessLogBadRLPData(t *testing.T) {
	assert := assert.New(t)

	spec := &StreamInfo{
		Timestamps: false,
	}
	stream := &eventStream{
		spec:        spec,
		eventStream: make(chan *eventData, 1),
	}
	eventABI := `{
    "name": "event1",
    "inputs": [
      {"name": "one", "type": "uint256"},
      {"name": "two", "type": "uint256"}
    ]
  }`
	var marshaling ethbinding.ABIElementMarshaling
	err := json.Unmarshal([]byte(eventABI), &marshaling)
	assert.NoError(err)
	event, _ := ethbind.API.ABIElementMarshalingToABIEvent(&marshaling)
	lp := &logProcessor{
		event:  event,
		stream: stream,
	}
	err = lp.processLogEntry(t.Name(), &logEntry{
		Data: "0x00",
	}, 0)

	assert.NoError(err)
	ev := <-stream.eventStream
	assert.Regexp("Failed to unpack values", ev.Data["error"])
}

func TestProcessLogSampleEvent(t *testing.T) {
	assert := assert.New(t)

	spec := &StreamInfo{
		Timestamps: false,
	}
	stream := &eventStream{
		spec:        spec,
		eventStream: make(chan *eventData, 1),
	}
	var marshaling ethbinding.ABIElementMarshaling
	json.Unmarshal([]byte(sampleEventABIAllIndexedNoData), &marshaling)
	event, _ := ethbind.API.ABIElementMarshalingToABIEvent(&marshaling)
	lp := &logProcessor{
		event:  event,
		stream: stream,
	}
	var l logEntry
	err := json.Unmarshal([]byte(sampleEventLogAllIndexedNoData), &l)
	assert.NoError(err)
	err = lp.processLogEntry(t.Name(), &l, 0)

	assert.NoError(err)
	ev := <-stream.eventStream
	assert.Equal(map[string]interface{}{
		"data1": "0x51b201b016025d42c9a0718b75aacc12b1e9c7f16e4bd2c6618aa944ca399156",
		"data2": "1000",
	}, ev.Data)
}

func TestProcessLogEntryRemovedWithConfirmationManager(t *testing.T) {
	assert := assert.New(t)

	bcm, _ := newTestBlockConfirmationManager(t, false)

	spec := &StreamInfo{
		Timestamps: false,
	}
	stream := &eventStream{
		spec:                    spec,
		decimalTransactionIndex: false,
	}

	eventABI := `{
		"name": "testEvent",
		"anonymous": true,
		"inputs": []
	}`
	var marshaling ethbinding.ABIElementMarshaling
	json.Unmarshal([]byte(eventABI), &marshaling)
	event, err := ethbind.API.ABIElementMarshalingToABIEvent(&marshaling)
	assert.NoError(err)

	lp := &logProcessor{
		event:               event,
		stream:              stream,
		confirmationManager: bcm,
	}
	err = lp.processLogEntry("ut", &logEntry{
		BlockNumber:      ethbinding.HexBigInt(*big.NewInt(255)),
		TransactionIndex: ethbinding.HexUint(10),
		Removed:          true,
	}, 2)
	assert.NoError(err)
	notification := <-bcm.bcmNotifications
	assert.Equal(bcmRemovedLog, notification.nType)
	assert.Equal("255", notification.event.BlockNumber)
	assert.Equal("0xa", notification.event.TransactionIndex)
	assert.Equal("2", notification.event.LogIndex)
	assert.Equal(uint64(255), notification.event.blockNumber)
	assert.Equal(uint64(10), notification.event.transactionIndex)
	assert.Equal(uint64(2), notification.event.logIndex)
}

func TestProcessLogEntryDispatchWithConfirmationManager(t *testing.T) {
	assert := assert.New(t)

	bcm, _ := newTestBlockConfirmationManager(t, false)

	spec := &StreamInfo{
		Timestamps: false,
	}
	stream := &eventStream{
		spec:                    spec,
		decimalTransactionIndex: true,
	}

	eventABI := `{
		"name": "testEvent",
		"anonymous": true,
		"inputs": []
	}`
	var marshaling ethbinding.ABIElementMarshaling
	json.Unmarshal([]byte(eventABI), &marshaling)
	event, err := ethbind.API.ABIElementMarshalingToABIEvent(&marshaling)
	assert.NoError(err)

	lp := &logProcessor{
		event:               event,
		stream:              stream,
		confirmationManager: bcm,
	}
	err = lp.processLogEntry("ut", &logEntry{
		BlockNumber:      ethbinding.HexBigInt(*big.NewInt(255)),
		TransactionIndex: ethbinding.HexUint(10),
	}, 2)
	assert.NoError(err)
	notification := <-bcm.bcmNotifications
	assert.Equal(bcmNewLog, notification.nType)
	assert.Equal("255", notification.event.BlockNumber)
	assert.Equal("10", notification.event.TransactionIndex)
	assert.Equal("2", notification.event.LogIndex)
	assert.Equal(uint64(255), notification.event.blockNumber)
	assert.Equal(uint64(10), notification.event.transactionIndex)
	assert.Equal(uint64(2), notification.event.logIndex)
}
