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
	"math/big"
	"strconv"
	"strings"
	"sync"

	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/eth"
	"github.com/hyperledger/firefly-ethconnect/internal/ethbind"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	log "github.com/sirupsen/logrus"
)

type logEntry struct {
	Address          ethbinding.Address   `json:"address"`
	BlockNumber      ethbinding.HexBigInt `json:"blockNumber"`
	TransactionIndex ethbinding.HexUint   `json:"transactionIndex"`
	TransactionHash  ethbinding.Hash      `json:"transactionHash"`
	Data             string               `json:"data"`
	Topics           []*ethbinding.Hash   `json:"topics"`
	Timestamp        uint64               `json:"timestamp,omitempty"`
}

type eventData struct {
	Address          string                 `json:"address"`
	BlockNumber      string                 `json:"blockNumber"`
	TransactionIndex string                 `json:"transactionIndex"`
	TransactionHash  string                 `json:"transactionHash"`
	Data             map[string]interface{} `json:"data"`
	SubID            string                 `json:"subId"`
	Signature        string                 `json:"signature"`
	LogIndex         string                 `json:"logIndex"`
	Timestamp        string                 `json:"timestamp,omitempty"`
	// Used for callback handling
	batchComplete func(*eventData)
}

type logProcessor struct {
	subID             string
	event             *ethbinding.ABIEvent
	stream            *eventStream
	blockHWM          big.Int
	highestDispatched big.Int
	hwnSync           sync.Mutex
}

func newLogProcessor(subID string, event *ethbinding.ABIEvent, stream *eventStream) *logProcessor {
	return &logProcessor{
		subID:  subID,
		event:  event,
		stream: stream,
	}
}

func (lp *logProcessor) batchComplete(newestEvent *eventData) {
	lp.hwnSync.Lock()
	i := new(big.Int)
	i.SetString(newestEvent.BlockNumber, 10)
	i.Add(i, big.NewInt(1)) // restart from the next block
	if i.Cmp(&lp.blockHWM) > 0 {
		lp.blockHWM.Set(i)
	}
	lp.hwnSync.Unlock()
	log.Debugf("%s: HWM: %s", lp.subID, lp.blockHWM.String())
}

func (lp *logProcessor) getBlockHWM() big.Int {
	lp.hwnSync.Lock()
	v := lp.blockHWM
	lp.hwnSync.Unlock()
	return v
}

func (lp *logProcessor) markNoEvents(blockNumber *big.Int) {
	lp.hwnSync.Lock()
	if lp.highestDispatched.Cmp(&lp.blockHWM) < 0 {
		// Nothing in-flight, its safe to update the HWM
		lp.blockHWM.Set(blockNumber)
		log.Debugf("%s: HWM: %s", lp.subID, lp.blockHWM.String())
	}
	lp.hwnSync.Unlock()
}

func (lp *logProcessor) initBlockHWM(intVal *big.Int) {
	lp.hwnSync.Lock()
	lp.blockHWM = *intVal
	lp.hwnSync.Unlock()
}

func (lp *logProcessor) processLogEntry(subInfo string, entry *logEntry, idx int) (err error) {

	var data []byte
	if strings.HasPrefix(entry.Data, "0x") {
		data, err = ethbind.API.HexDecode(entry.Data)
		if err != nil {
			return errors.Errorf(errors.EventStreamsLogDecode, subInfo, err)
		}
	}

	blockNumber := entry.BlockNumber.ToInt()
	result := &eventData{
		Address:          entry.Address.String(),
		BlockNumber:      blockNumber.String(),
		TransactionIndex: entry.TransactionIndex.String(),
		TransactionHash:  entry.TransactionHash.String(),
		Signature:        ethbind.API.ABIEventSignature(lp.event),
		Data:             make(map[string]interface{}),
		SubID:            lp.subID,
		LogIndex:         strconv.Itoa(idx),
		batchComplete:    lp.batchComplete,
	}
	if lp.stream.spec.Timestamps {
		result.Timestamp = strconv.FormatUint(entry.Timestamp, 10)
	}
	topicIdx := 0
	if !lp.event.Anonymous {
		topicIdx++ // first index is the hash of the event description
	}

	// We need split out the indexed args that we parse out of the topic, from the data args
	var dataArgs ethbinding.ABIArguments
	dataArgs = make([]ethbinding.ABIArgument, 0, len(lp.event.Inputs))
	for idx, input := range lp.event.Inputs {
		var val interface{}
		if input.Indexed {
			if topicIdx >= len(entry.Topics) {
				return errors.Errorf(errors.EventStreamsLogDecodeInsufficientTopics, subInfo, idx, ethbind.API.ABIEventSignature(lp.event))
			}
			topic := entry.Topics[topicIdx]
			topicIdx++
			if topic != nil {
				val = topicToValue(topic, &input)
			} else {
				val = nil
			}
			result.Data[input.Name] = val
		} else {
			dataArgs = append(dataArgs, input)
		}
	}

	// Retrieve the data args from the RLP and merge the results
	if len(dataArgs) > 0 {
		dataMap := eth.ProcessRLPBytes(dataArgs, data)
		for k, v := range dataMap {
			result.Data[k] = v
		}
	}

	// Ok, now we have the full event in a friendly map output. Pass it down to the event processor
	log.Infof("%s: Dispatching event. Address=%s BlockNumber=%s TxIndex=%s", subInfo, result.Address, result.BlockNumber, result.TransactionIndex)
	lp.hwnSync.Lock()
	if blockNumber.Cmp(&lp.highestDispatched) > 0 {
		lp.highestDispatched.Set(blockNumber)
	}
	lp.hwnSync.Unlock()
	lp.stream.handleEvent(result)
	return nil
}

func topicToValue(topic *ethbinding.Hash, input *ethbinding.ABIArgument) interface{} {
	switch input.Type.T {
	case ethbinding.IntTy, ethbinding.UintTy, ethbinding.BoolTy:
		h := ethbinding.HexBigInt{}
		h.UnmarshalText([]byte(topic.Hex()))
		bI, _ := ethbind.API.ParseBig256(topic.Hex())
		if input.Type.T == ethbinding.IntTy {
			// It will be a two's complement number, so needs to be interpretted
			bI = ethbind.API.S256(bI)
			return bI.String()
		} else if input.Type.T == ethbinding.BoolTy {
			return (bI.Uint64() != 0)
		}
		return bI.String()
	case ethbinding.AddressTy:
		topicBytes := topic.Bytes()
		addrBytes := topicBytes[len(topicBytes)-20:]
		return ethbind.API.BytesToAddress(addrBytes)
	default:
		// For all other types it is just a hash of the output for indexing, so we can only
		// logically return it as a hex string. The Solidity developer has to include
		// the same data a second type non-indexed to get the real value.
		return topic.String()
	}
}
