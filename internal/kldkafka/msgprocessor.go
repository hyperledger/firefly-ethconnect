// Copyright 2018 Kaleido, a ConsenSys business

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the icense is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldkafka

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
)

// MsgProcessor interface is called for each message, as is responsible
// for tracking all in-flight messages
type MsgProcessor interface {
	OnMessage(MsgContext)
	Init(kldeth.RPCClient, int)
}

type inflightTxn struct {
	from       string // normalized to 0x prefix and lower case
	nonce      int64
	msgContext MsgContext
	tx         *kldeth.Txn
	wg         sync.WaitGroup
}

func (i *inflightTxn) nonceNumber() json.Number {
	return json.Number(strconv.FormatInt(i.nonce, 10))
}

func (i *inflightTxn) String() string {
	txHash := ""
	if i.tx != nil {
		txHash = i.tx.Hash
	}
	return fmt.Sprintf("TX=%s CTX=%s", txHash, i.msgContext.String())
}

type msgProcessor struct {
	maxTXWaitTime      time.Duration
	inflightTxnsLock   *sync.Mutex
	inflightTxns       map[string][]*inflightTxn
	inflightTxnDelayer TxnDelayTracker
	rpc                kldeth.RPCClient
}

func newMsgProcessor() *msgProcessor {
	return &msgProcessor{
		inflightTxnsLock:   &sync.Mutex{},
		inflightTxns:       make(map[string][]*inflightTxn),
		inflightTxnDelayer: NewTxnDelayTracker(),
	}
}

func (p *msgProcessor) Init(rpc kldeth.RPCClient, maxTXWaitTime int) {
	p.rpc = rpc
	p.maxTXWaitTime = time.Duration(maxTXWaitTime) * time.Second
}

// OnMessage checks the type and dispatches to the correct logic
// ** From this point on the processor MUST ensure Reply is called
//    on msgContext eventually in all scenarios.
//    It cannot return an error synchronously from this function **
func (p *msgProcessor) OnMessage(msgContext MsgContext) {

	var unmarshalErr error
	headers := msgContext.Headers()
	switch headers.MsgType {
	case kldmessages.MsgTypeDeployContract:
		var deployContractMsg kldmessages.DeployContract
		if unmarshalErr = msgContext.Unmarshal(&deployContractMsg); unmarshalErr != nil {
			break
		}
		p.OnDeployContractMessage(msgContext, &deployContractMsg)
		break
	case kldmessages.MsgTypeSendTransaction:
		var sendTransactionMsg kldmessages.SendTransaction
		if unmarshalErr = msgContext.Unmarshal(&sendTransactionMsg); unmarshalErr != nil {
			break
		}
		p.OnSendTransactionMessage(msgContext, &sendTransactionMsg)
		break
	default:
		unmarshalErr = fmt.Errorf("Unknown message type '%s'", headers.MsgType)
	}
	// We must always send a reply
	if unmarshalErr != nil {
		msgContext.SendErrorReply(400, unmarshalErr)
	}

}

// newInflightWrapper uses the supplied transaction, the inflight txn list
// and the ethereum node's transction count to determine the right next
// nonce for the transaction.
// Builds a new wrapper containing this information, that can be added to
// the inflight list if the transaction is submitted
func (p *msgProcessor) newInflightWrapper(msgContext MsgContext, suppliedFrom string, suppliedNonce json.Number) (inflight *inflightTxn, err error) {

	inflight = &inflightTxn{
		msgContext: msgContext,
	}

	// Validate the from address, and normalize to lower case with 0x prefix
	from, err := kldutils.StrToAddress("from", suppliedFrom)
	if err != nil {
		return
	}
	inflight.from = strings.ToLower(from.Hex())

	// The user can supply a nonce and manage them externally, using their own
	// application-side list of transactions, to prevent the possibility of
	// duplication that exists when dynamically calculating the nonce
	if suppliedNonce != "" {
		if inflight.nonce, err = suppliedNonce.Int64(); err != nil {
			err = fmt.Errorf("Converting supplied 'nonce' to integer: %s", err)
		}
		return
	}

	// Hold the lock just long enough to check the currently inflight txns.
	// This function is always called on the OnMessage goroutine, but
	// other goroutines might be trying to complete transactions so we don't
	// hold it while we're querying the nonce
	var highestNonce int64
	p.inflightTxnsLock.Lock()
	if inflightForAddr, exists := p.inflightTxns[inflight.from]; exists {
		for _, inflight := range inflightForAddr {
			if inflight.nonce > highestNonce {
				highestNonce = inflight.nonce
			}
		}
	}
	p.inflightTxnsLock.Unlock()

	// If we found a nonce, return one higher.
	if highestNonce > 0 {
		inflight.nonce = highestNonce + 1
		return
	}

	// Otherwise we need to do a dirty read from the ethereum client,
	// which will be ok as long as we're the only JSON/RPC writing to
	// this address. But if we're competing with other transactions
	// we need to accept the possibility of 'nonce too low'
	inflight.nonce, err = kldeth.GetTransactionCount(p.rpc, &from, "latest")
	return
}

// waitForCompletion is the goroutine to track a transaction through
// to completion and send the result
func (p *msgProcessor) waitForCompletion(iTX *inflightTxn, initialWaitDelay time.Duration) {

	// The initial delay is passed in, based on updates from all the other
	// go routines that are tracking transactions. The idea is to minimize
	// both latency beyond the block period, and avoiding spamming the node
	// with REST calls for long block periods, or when there is a backlog
	replyWaitStart := time.Now()
	time.Sleep(initialWaitDelay)

	var isMined, timedOut bool
	var err error
	var retries int
	for !isMined && !timedOut {

		if isMined, err = iTX.tx.GetTXReceipt(p.rpc); err != nil {
			// We wait even on connectivity errors, as we've submitted the transaction and
			// we want to provide a receipt if connectivity resumes within the timeout
			log.Infof("Failed to get receipt for %s (retries=%d): %s", iTX, retries, err)
		}

		elapsed := time.Now().Sub(replyWaitStart)
		timedOut = elapsed > p.maxTXWaitTime
		if !isMined && !timedOut {
			// Need to have the inflight lock to calculate the delay, but not
			// while we're waiting
			p.inflightTxnsLock.Lock()
			delayBeforeRetry := p.inflightTxnDelayer.GetRetryDelay(initialWaitDelay, retries+1)
			p.inflightTxnsLock.Unlock()

			log.Infof("Recept not available after %.2fs (retries=%d): %s", elapsed.Seconds(), retries, iTX)
			time.Sleep(delayBeforeRetry)
			retries++
		}
	}

	if timedOut {
		if err != nil {
			iTX.msgContext.SendErrorReply(500, fmt.Errorf("Error obtaining transaction receipt (%d retries): %s", retries, err))
		} else {
			iTX.msgContext.SendErrorReply(408, fmt.Errorf("Timed out waiting for transaction receipt"))
		}
	} else {
		// Build our reply
		receipt := iTX.tx.Receipt
		var reply kldmessages.TransactionReceipt
		if receipt.Status != nil && receipt.Status.ToInt().Int64() > 0 {
			reply.Headers.MsgType = kldmessages.MsgTypeTransactionSuccess
		} else {
			reply.Headers.MsgType = kldmessages.MsgTypeTransactionFailure
		}
		reply.BlockHash = receipt.BlockHash
		reply.BlockNumberHex = receipt.BlockNumber
		if receipt.BlockNumber != nil {
			reply.BlockNumberStr = receipt.BlockNumber.ToInt().Text(10)
		}
		reply.ContractAddress = receipt.ContractAddress
		reply.CumulativeGasUsedHex = receipt.CumulativeGasUsed
		if receipt.CumulativeGasUsed != nil {
			reply.CumulativeGasUsedStr = receipt.CumulativeGasUsed.ToInt().Text(10)
		}
		reply.From = receipt.From
		reply.GasUsedHex = receipt.GasUsed
		if receipt.GasUsed != nil {
			reply.GasUsedStr = receipt.GasUsed.ToInt().Text(10)
		}
		reply.StatusHex = receipt.Status
		if receipt.Status != nil {
			reply.StatusStr = receipt.Status.ToInt().Text(10)
		}
		reply.To = receipt.To
		reply.TransactionHash = receipt.TransactionHash
		reply.TransactionIndexHex = receipt.TransactionIndex
		if receipt.TransactionIndex != nil {
			reply.TransactionIndexStr = strconv.FormatUint(uint64(*receipt.TransactionIndex), 10)
		}
		iTX.msgContext.Reply(&reply)
	}

	iTX.wg.Done()
}

// addInflight adds a transction to the inflight list, and kick off
// a goroutine to check for its completion and send the result
func (p *msgProcessor) addInflight(inflight *inflightTxn, tx *kldeth.Txn) {

	// Add the inflight transaction to our tracking structure
	p.inflightTxnsLock.Lock()
	inflight.tx = tx
	inflightForAddr, exists := p.inflightTxns[inflight.from]
	if !exists {
		inflightForAddr = []*inflightTxn{}
	}
	p.inflightTxns[inflight.from] = append(inflightForAddr, inflight)
	initialWaitDelay := p.inflightTxnDelayer.GetInitialDelay() // Must call under lock
	p.inflightTxnsLock.Unlock()

	// Kick off the goroutine to track it to completion
	inflight.wg.Add(1)
	go p.waitForCompletion(inflight, initialWaitDelay)

}

func (p *msgProcessor) OnDeployContractMessage(msgContext MsgContext, msg *kldmessages.DeployContract) {

	inflightWrapper, err := p.newInflightWrapper(msgContext, msg.From, msg.Nonce)
	if err != nil {
		msgContext.SendErrorReply(400, err)
		return
	}
	msg.Nonce = inflightWrapper.nonceNumber()

	tx, err := kldeth.NewContractDeployTxn(msg)
	if err != nil {
		msgContext.SendErrorReply(400, err)
		return
	}

	if err = tx.Send(p.rpc); err != nil {
		msgContext.SendErrorReply(400, err)
		return
	}

	p.addInflight(inflightWrapper, tx)
}

func (p *msgProcessor) OnSendTransactionMessage(msgContext MsgContext, msg *kldmessages.SendTransaction) {

	inflightWrapper, err := p.newInflightWrapper(msgContext, msg.From, msg.Nonce)
	if err != nil {
		msgContext.SendErrorReply(400, err)
		return
	}
	msg.Nonce = inflightWrapper.nonceNumber()

	tx, err := kldeth.NewSendTxn(msg)
	if err != nil {
		msgContext.SendErrorReply(400, err)
		return
	}

	if err = tx.Send(p.rpc); err != nil {
		msgContext.SendErrorReply(400, err)
		return
	}

	p.addInflight(inflightWrapper, tx)
}
