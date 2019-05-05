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
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/kaleido-io/ethconnect/internal/kldbind"
)

type logEntry struct {
	Address          kldbind.Address   `json:"address"`
	BlockNumber      kldbind.HexBigInt `json:"blockNumber"`
	TransactionIndex json.Number       `json:"transactionIndex"`
	TransactionHash  kldbind.Hash      `json:"transactionHash"`
	Data             string            `json:"data"`
	Topics           []*kldbind.Hash   `json:"topics"`
}

type eventData struct {
	Address          string                 `json:"address"`
	BlockNumber      string                 `json:"blockNumber"`
	TransactionIndex string                 `json:"transactionIndex"`
	TransactionHash  string                 `json:"transactionHash"`
	Data             map[string]interface{} `json:"data"`
}

func processLogEntry(entry *logEntry, event *kldbind.ABIEvent) (result *eventData, err error) {
	// if strings.HasPrefix(entry.Data, "0x") {
	// 	data, err := kldbind.HexDecode(entry.Data)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("Failed to decode data: %s", err)
	// 	}
	// }

	result = &eventData{
		Address:          entry.Address.String(),
		BlockNumber:      entry.BlockNumber.ToInt().String(),
		TransactionIndex: entry.TransactionIndex.String(),
		TransactionHash:  entry.TransactionHash.String(),
		Data:             make(map[string]interface{}),
	}
	topicIdx := 0
	if !event.Anonymous {
		topicIdx++ // first index is the hash of the event description
	}
	for idx, input := range event.Inputs {
		var val interface{}
		if input.Indexed {
			if topicIdx >= len(entry.Topics) {
				return nil, fmt.Errorf("Ran out of topics for indexed fields at field %d of %s", idx, event)
			}
			topic := entry.Topics[topicIdx]
			topicIdx++
			if topic != nil {
				if val, err = topicToValue(topic, &input); err != nil {
					return nil, fmt.Errorf("Failed to process topic value %s for input %d", topic, idx)
				}
			}
		}
		result.Data[event.Name] = val
	}
	return result, nil
}

func topicToValue(topic *kldbind.Hash, input *kldbind.ABIArgument) (interface{}, error) {
	switch input.Type.T {
	case kldbind.IntTy, kldbind.UintTy, kldbind.BoolTy:
		h := kldbind.HexBigInt{}
		h.UnmarshalText([]byte(topic.Hex()))
		bI, ok := math.ParseBig256(topic.Hex())
		if !ok {
			return nil, fmt.Errorf("Could not parse number: %s", topic.Hex())
		}
		if input.Type.T == kldbind.IntTy {
			// It will be a two's complement number, so needs to be interpretted
			bI = math.S256(bI)
			return bI, nil
		} else if input.Type.T == kldbind.BoolTy {
			return (bI.Uint64() != 0), nil
		}
		return bI, nil
	case kldbind.AddressTy:
		topicBytes := topic.Bytes()
		addrBytes := topicBytes[len(topicBytes)-20:]
		return kldbind.BytesToAddress(addrBytes), nil
	default:
		return nil, nil
	}
}
