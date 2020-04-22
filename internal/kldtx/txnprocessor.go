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

package kldtx

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/spf13/cobra"

	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
)

const (
	defaultSendConcurrency = 1
)

// TxnProcessor interface is called for each message, as is responsible
// for tracking all in-flight messages
type TxnProcessor interface {
	OnMessage(TxnContext)
	Init(kldeth.RPCClient)
}

var highestID = 1000000

type inflightTxn struct {
	id               int
	from             string // normalized to 0x prefix and lower case
	nodeAssignNonce  bool
	nonce            int64
	privacyGroupID   string
	initialWaitDelay time.Duration
	txnContext       TxnContext
	tx               *kldeth.Txn
	wg               sync.WaitGroup
	registerAs       string // passed from request to reply
	rpc              kldeth.RPCClient
	signer           kldeth.TXSigner
	gapFillSucceeded bool
	gapFillTxHash    string
}

func (i *inflightTxn) nonceNumber() json.Number {
	return json.Number(strconv.FormatInt(i.nonce, 10))
}

func (i *inflightTxn) String() string {
	txHash := ""
	if i.tx != nil {
		txHash = i.tx.Hash
	}
	return fmt.Sprintf("TX=%s CTX=%s", txHash, i.txnContext.String())
}

// TxnProcessorConf configuration for the message processor
type TxnProcessorConf struct {
	AlwaysManageNonce  bool            `json:"alwaysManageNonce"`
	AttemptGapFill     bool            `json:"attemptGapFill"`
	MaxTXWaitTime      int             `json:"maxTXWaitTime"`
	SendConcurrency    int             `json:"sendConcurrency"`
	OrionPrivateAPIS   bool            `json:"orionPrivateAPIs"`
	HexValuesInReceipt bool            `json:"hexValuesInReceipt"`
	AddressBookConf    AddressBookConf `json:"addressBook"`
	HDWalletConf       HDWalletConf    `json:"hdWallet"`
}

type txnProcessor struct {
	maxTXWaitTime      time.Duration
	inflightTxnsLock   *sync.Mutex
	inflightTxns       map[string][]*inflightTxn
	inflightTxnDelayer TxnDelayTracker
	rpc                kldeth.RPCClient
	addressBook        AddressBook
	hdwallet           HDWallet
	conf               *TxnProcessorConf
	rpcConf            *kldeth.RPCConf
	concurrencySlots   chan bool
}

// NewTxnProcessor constructor for message procss
func NewTxnProcessor(conf *TxnProcessorConf, rpcConf *kldeth.RPCConf) TxnProcessor {
	if conf.SendConcurrency == 0 {
		conf.SendConcurrency = defaultSendConcurrency
	}
	p := &txnProcessor{
		inflightTxnsLock:   &sync.Mutex{},
		inflightTxns:       make(map[string][]*inflightTxn),
		inflightTxnDelayer: NewTxnDelayTracker(),
		conf:               conf,
		rpcConf:            rpcConf,
		concurrencySlots:   make(chan bool, conf.SendConcurrency),
	}
	return p
}

func (p *txnProcessor) Init(rpc kldeth.RPCClient) {
	p.rpc = rpc
	p.maxTXWaitTime = time.Duration(p.conf.MaxTXWaitTime) * time.Second
	if p.conf.AddressBookConf.AddressbookURLPrefix != "" {
		p.addressBook = NewAddressBook(&p.conf.AddressBookConf, p.rpcConf)
	}
	if p.conf.HDWalletConf.URLTemplate != "" {
		p.hdwallet = newHDWallet(&p.conf.HDWalletConf)
	}
}

// CobraInitTxnProcessor sets the standard command-line parameters for the txnprocessor
func CobraInitTxnProcessor(cmd *cobra.Command, txconf *TxnProcessorConf) {
	cmd.Flags().IntVarP(&txconf.MaxTXWaitTime, "tx-timeout", "x", kldutils.DefInt("ETH_TX_TIMEOUT", 0), "Maximum wait time for an individual transaction (seconds)")
	cmd.Flags().BoolVarP(&txconf.HexValuesInReceipt, "hex-values", "H", false, "Include hex values for large numbers in receipts (as well as numeric strings)")
	cmd.Flags().BoolVarP(&txconf.AlwaysManageNonce, "predict-nonces", "P", false, "Predict the next nonce before sending (default=false for node-signed txns)")
	cmd.Flags().BoolVarP(&txconf.OrionPrivateAPIS, "orion-privapi", "G", false, "Use Orion JSON/RPC API semantics for private transactions")
	return
}

// OnMessage checks the type and dispatches to the correct logic
// ** From this point on the processor MUST ensure Reply is called
//    on txnContext eventually in all scenarios.
//    It cannot return an error synchronously from this function **
func (p *txnProcessor) OnMessage(txnContext TxnContext) {

	var unmarshalErr error
	headers := txnContext.Headers()
	log.Debugf("Processing %+v", headers)
	switch headers.MsgType {
	case kldmessages.MsgTypeDeployContract:
		var deployContractMsg kldmessages.DeployContract
		if unmarshalErr = txnContext.Unmarshal(&deployContractMsg); unmarshalErr != nil {
			break
		}
		p.OnDeployContractMessage(txnContext, &deployContractMsg)
		break
	case kldmessages.MsgTypeSendTransaction:
		var sendTransactionMsg kldmessages.SendTransaction
		if unmarshalErr = txnContext.Unmarshal(&sendTransactionMsg); unmarshalErr != nil {
			break
		}
		p.OnSendTransactionMessage(txnContext, &sendTransactionMsg)
		break
	default:
		unmarshalErr = klderrors.Errorf(klderrors.TransactionSendMsgTypeUnknown, headers.MsgType)
	}
	// We must always send a reply
	if unmarshalErr != nil {
		txnContext.SendErrorReply(400, unmarshalErr)
	}

}

// newInflightWrapper uses the supplied transaction, the inflight txn list
// and the ethereum node's transction count to determine the right next
// nonce for the transaction.
// Builds a new wrapper containing this information, that can be added to
// the inflight list if the transaction is submitted
func (p *txnProcessor) addInflightWrapper(txnContext TxnContext, msg *kldmessages.TransactionCommon) (inflight *inflightTxn, err error) {

	inflight = &inflightTxn{
		txnContext: txnContext,
	}

	// Use the correct RPC for sending transactions
	inflight.rpc = p.rpc
	if hdWalletRequest := IsHDWalletRequest(msg.From); hdWalletRequest != nil {
		if p.hdwallet == nil {
			return nil, klderrors.Errorf(klderrors.HDWalletSigningNoConfig)
		}
		if inflight.signer, err = p.hdwallet.SignerFor(hdWalletRequest); err != nil {
			return
		}
		msg.From = inflight.signer.Address()
	} else if p.addressBook != nil {
		if inflight.rpc, err = p.addressBook.lookup(txnContext.Context(), msg.From); err != nil {
			return
		}
	}

	// Validate the from address, and normalize to lower case with 0x prefix
	from, err := kldutils.StrToAddress("from", msg.From)
	if err != nil {
		return
	}
	inflight.from = strings.ToLower(from.Hex())

	// Need to resolve privateFrom/privateFor to a privacyGroupID for Orion
	if p.conf.OrionPrivateAPIS {
		if msg.PrivacyGroupID != "" && len(msg.PrivateFor) > 0 {
			err = klderrors.Errorf(klderrors.TransactionSendPrivateForAndPrivacyGroup)
			return
		} else if msg.PrivacyGroupID != "" {
			inflight.privacyGroupID = msg.PrivacyGroupID
		} else if len(msg.PrivateFor) > 0 {
			if inflight.privacyGroupID, err = kldeth.GetOrionPrivacyGroup(txnContext.Context(), p.rpc, &from, msg.PrivateFrom, msg.PrivateFor); err != nil {
				return
			}
		}
	}

	nodeAssignNonce := inflight.signer == nil && !p.conf.AlwaysManageNonce

	// Hold the lock just while we're adding it to the map and dealing with nonce checking.
	p.inflightTxnsLock.Lock()

	// The user can supply a nonce and manage them externally, using their own
	// application-side list of transactions, to prevent the possibility of
	// duplication that exists when dynamically calculating the nonce
	inflight.id = highestID
	highestID++
	var highestNonce int64 = -1
	suppliedNonce := msg.Nonce
	inflightForAddr, exists := p.inflightTxns[inflight.from]

	if !nodeAssignNonce && suppliedNonce == "" {
		// Check the currently inflight txns to see if we have a high nonce to use without
		// needing to query the node to find the highest nonce.
		if exists {
			for _, alreadyInflight := range inflightForAddr {
				if alreadyInflight.nonce > highestNonce {
					highestNonce = alreadyInflight.nonce
				}
			}
		}
	}

	// We want to submit this transaction with the next nonce in the chain.
	// If this is a node-signed transaction, then we can ask the node
	// to simply use the next available nonce.
	// We provide an override to force the Go code to always assign the nonce.
	fromNode := false
	if suppliedNonce != "" {
		if inflight.nonce, err = suppliedNonce.Int64(); err != nil {
			err = klderrors.Errorf(klderrors.TransactionSendBadNonce, err)
			return
		}
	} else if p.conf.OrionPrivateAPIS && (len(msg.PrivateFor) > 0 || msg.PrivacyGroupID != "") {
		// If are using orion private transactions, then we need the private TX
		// group ID and nonce (the public transaction will be submitted by the pantheon node)
		// Note: We do not have highestNonce calculation for in-flight private transactions,
		//       so attempting to submit more than one per block currently will FAIL
		if inflight.nonce, err = kldeth.GetOrionTXCount(txnContext.Context(), p.rpc, &from, inflight.privacyGroupID); err != nil {
			p.inflightTxnsLock.Unlock()
			return
		}
		fromNode = true
	} else if highestNonce >= 0 {
		// If we found a nonce in-flight in memory, return one higher.
		inflight.nonce = highestNonce + 1
	} else if nodeAssignNonce {
		// We've been asked to defer to the node for signing, and are not performing HD Wallet signing
		inflight.nodeAssignNonce = true
	} else {
		// Alternatively we do a dirty read from the node of the highest comitted
		// transaction. This will be ok as long as we're the only JSON/RPC writing to
		// this address. But if we're competing with other transactions
		// we need to accept the possibility of 'replacement transaction underpriced'
		// (or if gas price is being varied by the submitter the potential of
		// overwriting a transaction)
		if inflight.nonce, err = kldeth.GetTransactionCount(txnContext.Context(), p.rpc, &from, "pending"); err != nil {
			p.inflightTxnsLock.Unlock()
			return
		}
		fromNode = true
	}

	// Add the inflight transaction to our tracking structure
	if !exists {
		inflightForAddr = []*inflightTxn{}
	}
	before := len(inflightForAddr)
	p.inflightTxns[inflight.from] = append(inflightForAddr, inflight)
	inflight.initialWaitDelay = p.inflightTxnDelayer.GetInitialDelay() // Must call under lock

	// Clear lock beofre logging
	p.inflightTxnsLock.Unlock()

	log.Infof("In-flight %d added. nonce=%d addr=%s before=%d (node=%t)", inflight.id, inflight.nonce, inflight.from, before, fromNode)

	return
}

func (p *txnProcessor) cancelInFlight(inflight *inflightTxn, gapPotential bool) {
	var before, after int
	var higherNonceInflight = int64(-1)
	p.inflightTxnsLock.Lock()
	if inflightForAddr, exists := p.inflightTxns[inflight.from]; exists {
		before = len(inflightForAddr)
		for idx, alreadyInflight := range inflightForAddr {
			if alreadyInflight.id == inflight.id {
				inflightForAddr = append(inflightForAddr[0:idx], inflightForAddr[idx+1:]...)
				p.inflightTxns[inflight.from] = inflightForAddr
				break
			}
		}
		after = len(inflightForAddr)
		// Check the transactions that are left, to see if any nonce is higher
		if gapPotential && !inflight.nodeAssignNonce {
			for _, alreadyInflight := range inflightForAddr {
				if alreadyInflight.nonce > inflight.nonce && alreadyInflight.nonce > higherNonceInflight {
					higherNonceInflight = alreadyInflight.nonce
				}
			}
		}
	}
	p.inflightTxnsLock.Unlock()

	log.Infof("In-flight %d complete. nonce=%d addr=%s before=%d after=%d", inflight.id, inflight.nonce, inflight.from, before, after)

	// If we've got a gap potential, we need to submit a gap-fill TX
	if higherNonceInflight > 0 {
		log.Warnf("Potential nonce gap. Nonce %d failed to send. Nonce %d in-flight", inflight.nonce, higherNonceInflight)
		p.submitGapFillTX(inflight)
	}

}

// submitGapFillTX attempts to send a zero gas, no data, transfer of zero ether transaction
// to the from address, for the purpose of filling a nonce gap and allowing subsequent transactions
// to complete. Only
func (p *txnProcessor) submitGapFillTX(inflight *inflightTxn) {
	if p.conf.AttemptGapFill {
		tx, err := kldeth.NewNilTX(inflight.from, inflight.nonce, inflight.signer)
		if err == nil {
			inflight.gapFillTxHash = tx.EthTX.Hash().String()
			err = tx.Send(inflight.txnContext.Context(), inflight.rpc)
			if err != nil {
				inflight.gapFillSucceeded = false
				log.Warnf("Submission of gap-fill TX '%s' failed: %s", tx.Hash, err)
			} else {
				inflight.gapFillSucceeded = true
				log.Infof("Submission of gap-fill TX '%s' completed", tx.Hash)
			}
		}
	}
}

// waitForCompletion is the goroutine to track a transaction through
// to completion and send the result
func (p *txnProcessor) waitForCompletion(inflight *inflightTxn, initialWaitDelay time.Duration) {

	// The initial delay is passed in, based on updates from all the other
	// go routines that are tracking transactions. The idea is to minimize
	// both latency beyond the block period, and avoiding spamming the node
	// with REST calls for long block periods, or when there is a backlog
	replyWaitStart := time.Now().UTC()
	time.Sleep(initialWaitDelay)

	var isMined, timedOut bool
	var err error
	var retries int
	var elapsed time.Duration
	for !isMined && !timedOut {

		if isMined, err = inflight.tx.GetTXReceipt(inflight.txnContext.Context(), p.rpc); err != nil {
			// We wait even on connectivity errors, as we've submitted the transaction and
			// we want to provide a receipt if connectivity resumes within the timeout
			log.Infof("Failed to get receipt for %s (retries=%d): %s", inflight, retries, err)
		}

		elapsed = time.Now().UTC().Sub(replyWaitStart)
		timedOut = elapsed > p.maxTXWaitTime
		if !isMined && !timedOut {
			// Need to have the inflight lock to calculate the delay, but not
			// while we're waiting
			p.inflightTxnsLock.Lock()
			delayBeforeRetry := p.inflightTxnDelayer.GetRetryDelay(initialWaitDelay, retries+1)
			p.inflightTxnsLock.Unlock()

			log.Debugf("Recept not available after %.2fs (retries=%d): %s", elapsed.Seconds(), retries, inflight)
			time.Sleep(delayBeforeRetry)
			retries++
		}
	}

	if timedOut {
		if err != nil {
			inflight.txnContext.SendErrorReplyWithTX(500, klderrors.Errorf(klderrors.TransactionSendReceiptCheckError, retries, err), inflight.tx.Hash)
		} else {
			inflight.txnContext.SendErrorReplyWithTX(408, klderrors.Errorf(klderrors.TransactionSendReceiptCheckTimeout), inflight.tx.Hash)
		}
	} else {
		// Update the stats
		p.inflightTxnsLock.Lock()
		p.inflightTxnDelayer.ReportSuccess(elapsed)
		p.inflightTxnsLock.Unlock()

		receipt := inflight.tx.Receipt
		isSuccess := (receipt.Status != nil && receipt.Status.ToInt().Int64() > 0)
		log.Infof("Receipt for %s obtained after %.2fs Success=%t", inflight.tx.Hash, elapsed.Seconds(), isSuccess)

		// Build our reply
		var reply kldmessages.TransactionReceipt
		if isSuccess {
			reply.Headers.MsgType = kldmessages.MsgTypeTransactionSuccess
		} else {
			reply.Headers.MsgType = kldmessages.MsgTypeTransactionFailure
		}
		reply.BlockHash = receipt.BlockHash
		if p.conf.HexValuesInReceipt {
			reply.BlockNumberHex = receipt.BlockNumber
		}
		if receipt.BlockNumber != nil {
			reply.BlockNumberStr = receipt.BlockNumber.ToInt().Text(10)
		}
		reply.ContractAddress = receipt.ContractAddress
		reply.RegisterAs = inflight.registerAs
		if p.conf.HexValuesInReceipt {
			reply.CumulativeGasUsedHex = receipt.CumulativeGasUsed
		}
		if receipt.CumulativeGasUsed != nil {
			reply.CumulativeGasUsedStr = receipt.CumulativeGasUsed.ToInt().Text(10)
		}
		reply.From = receipt.From
		if p.conf.HexValuesInReceipt {
			reply.GasUsedHex = receipt.GasUsed
		}
		if receipt.GasUsed != nil {
			reply.GasUsedStr = receipt.GasUsed.ToInt().Text(10)
		}
		nonceHex := hexutil.Uint64(inflight.nonce)
		if p.conf.HexValuesInReceipt {
			reply.NonceHex = &nonceHex
		}
		reply.NonceStr = strconv.FormatInt(inflight.nonce, 10)
		if p.conf.HexValuesInReceipt {
			reply.StatusHex = receipt.Status
		}
		if receipt.Status != nil {
			reply.StatusStr = receipt.Status.ToInt().Text(10)
		}
		reply.To = receipt.To
		reply.TransactionHash = receipt.TransactionHash
		if p.conf.HexValuesInReceipt {
			reply.TransactionIndexHex = receipt.TransactionIndex
		}
		if receipt.TransactionIndex != nil {
			reply.TransactionIndexStr = strconv.FormatUint(uint64(*receipt.TransactionIndex), 10)
		}

		inflight.txnContext.Reply(&reply)
	}

	// We've submitted the transaction (even if we didn't get a receipt within our timeout)
	// it is not a gap potential.
	p.cancelInFlight(inflight, false)
	inflight.wg.Done()
}

// addInflight adds a transction to the inflight list, and kick off
// a goroutine to check for its completion and send the result
func (p *txnProcessor) trackMining(inflight *inflightTxn, tx *kldeth.Txn) {

	// Kick off the goroutine to track it to completion
	inflight.tx = tx
	inflight.wg.Add(1)
	go p.waitForCompletion(inflight, inflight.initialWaitDelay)

}

func (p *txnProcessor) OnDeployContractMessage(txnContext TxnContext, msg *kldmessages.DeployContract) {

	inflight, err := p.addInflightWrapper(txnContext, &msg.TransactionCommon)
	if err != nil {
		txnContext.SendErrorReply(400, err)
		return
	}
	inflight.registerAs = msg.RegisterAs
	msg.Nonce = inflight.nonceNumber()

	tx, err := kldeth.NewContractDeployTxn(msg, inflight.signer)
	if err != nil {
		p.cancelInFlight(inflight, false) // No gap potential, we haven't submitted
		txnContext.SendErrorReply(400, err)
		return
	}

	p.sendTransactionCommon(txnContext, inflight, tx)
}

func (p *txnProcessor) OnSendTransactionMessage(txnContext TxnContext, msg *kldmessages.SendTransaction) {

	inflight, err := p.addInflightWrapper(txnContext, &msg.TransactionCommon)
	if err != nil {
		txnContext.SendErrorReply(400, err)
		return
	}
	msg.Nonce = inflight.nonceNumber()

	tx, err := kldeth.NewSendTxn(msg, inflight.signer)
	if err != nil {
		p.cancelInFlight(inflight, false) // No gap potential, we haven't submitted
		txnContext.SendErrorReply(400, err)
		return
	}

	p.sendTransactionCommon(txnContext, inflight, tx)
}

func (p *txnProcessor) sendTransactionCommon(txnContext TxnContext, inflight *inflightTxn, tx *kldeth.Txn) {
	tx.OrionPrivateAPIS = p.conf.OrionPrivateAPIS
	tx.PrivacyGroupID = inflight.privacyGroupID
	tx.NodeAssignNonce = inflight.nodeAssignNonce

	if p.conf.SendConcurrency > 1 {
		// The above must happen synchronously for each partition in Kafka - as it is where we assign the nonce.
		// However, the send to the node can happen at high concurrency.
		p.concurrencySlots <- true
		go p.sendAndTrackMining(txnContext, inflight, tx)
	} else {
		// For the special case of 1 we do it synchronously, so we don't assign the next nonce until we've sent this one
		p.sendAndTrackMining(txnContext, inflight, tx)
	}
}

func (p *txnProcessor) sendAndTrackMining(txnContext TxnContext, inflight *inflightTxn, tx *kldeth.Txn) {
	err := tx.Send(txnContext.Context(), inflight.rpc)
	if p.conf.SendConcurrency > 1 {
		<-p.concurrencySlots // return our slot as soon as send is complete, to let an awaiting send go
	}
	if err != nil {
		// Now we potentially have a gap - if we allocated the nonce, but failed to send
		p.cancelInFlight(inflight, true)
		txnContext.SendErrorReplyWithGapFill(400, err, inflight.gapFillTxHash, inflight.gapFillSucceeded)
		return
	}

	p.trackMining(inflight, tx)
}
