// Copyright 2018 Kaleido, a ConsenSys business

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
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/kaleido-io/ethconnect/pkg/kldmessages"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	log "github.com/sirupsen/logrus"
)

// KldTx wraps an ethereum transaction, along with the logic to send it over
// JSON/RPC to a node
type KldTx struct {
	EthTransaction *types.Transaction
}

// NewContractDeployTxn builds a new ethereum transaction from the supplied
// SendTranasction message
func NewContractDeployTxn(msg kldmessages.DeployContract) (tx *KldTx, err error) {

	// Compile the solidity contract
	compiledSolidity, err := CompileContract(msg.Solidity, msg.ContractName)
	if err != nil {
		return
	}

	// Build correctly typed args for the ethereum call
	typedArgs, err := generateTypedArgs(compiledSolidity.ABI.Constructor, msg.Parameters)
	if err != nil {
		return
	}

	// Pack the arguments
	packedCall, err := compiledSolidity.ABI.Pack("", typedArgs...)
	if err != nil {
		err = fmt.Errorf("Packing arguments for constructor: %s", err)
		return
	}

	// Generate the ethereum transaction
	ethTx, err := msg.ToEthTransaction(packedCall)
	if err != nil {
		return
	}

	log.Infof("Contract ready to call. Hash=%s", ethTx.Hash().Hex())

	return
}

// GenerateTypedArgs parses string arguments into a range of types to pass to the ABI call
func generateTypedArgs(method abi.Method, params []interface{}) (typedArgs []interface{}, err error) {

	log.Debug("Parsing args for function: ", method)
	for idx, inputArg := range method.Inputs {
		if idx >= len(params) {
			err = fmt.Errorf("Function '%s': Requires %d args (supplied=%d)", method.Name, len(method.Inputs), len(params))
			return
		}
		requiredType := inputArg.Type.String()
		suppliedType := reflect.TypeOf(params[idx])
		switch requiredType {
		case "string":
			if suppliedType.Kind() == reflect.String {
				typedArgs = append(typedArgs, params[idx].(string))
			} else {
				err = fmt.Errorf("Function '%s' param %d: Must be a string", method.Name, idx)
			}
			break
		case "int256", "uint256":
			if suppliedType.Kind() == reflect.String {
				bigInt := big.NewInt(0)
				if _, ok := bigInt.SetString(params[idx].(string), 10); !ok {
					err = fmt.Errorf("Function '%s' param %d: Could not be converted to a number", method.Name, idx)
					break
				}
				typedArgs = append(typedArgs, bigInt)
			} else if suppliedType.Kind() == reflect.Float64 {
				typedArgs = append(typedArgs, big.NewInt(int64(params[idx].(float64))))
			} else {
				err = fmt.Errorf("Function '%s' param %d is a %s: Must supply a number or a string", method.Name, idx, requiredType)
			}
			break
		case "bool":
			if suppliedType.Kind() == reflect.String {
				typedArgs = append(typedArgs, strings.ToLower(params[idx].(string)) == "true")
			} else if suppliedType.Kind() == reflect.Bool {
				typedArgs = append(typedArgs, params[idx].(bool))
			} else {
				err = fmt.Errorf("Function '%s' param %d is a %s: Must supply a boolean or a string", method.Name, idx, requiredType)
			}
			break
		case "address":
			if suppliedType.Kind() == reflect.String {
				if !common.IsHexAddress(params[idx].(string)) {
					err = fmt.Errorf("Function '%s' param %d: Could not be converted to a hex address", method.Name, idx)
					break
				}
				typedArgs = append(typedArgs, common.HexToAddress(params[idx].(string)))
			} else {
				err = fmt.Errorf("Function '%s' param %d is a %s: Must supply a boolean or a string", method.Name, idx, requiredType)
			}
			break
		default:
			return nil, fmt.Errorf("Type %s is not yet supported", inputArg.Type)
		}
		if err != nil {
			log.Errorf("%s [Required=%s Supplied=%s Value=%s]", err, requiredType, suppliedType, params[idx])
			return
		}
	}

	return
}

// // sendTransaction sends an individual transaction, choosing external or internal signing
// func (e *Eth) sendTransaction(tx *types.Transaction) (string, error) {
// 	start := time.Now()

// 	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
// 	defer cancel()

// 	var err error
// 	var txHash string
// 	if w.Exerciser.ExternalSign {
// 		txHash, err = w.signAndSendTxn(ctx, tx)
// 	} else {
// 		txHash, err = w.sendUnsignedTxn(ctx, tx)
// 	}
// 	callTime := time.Now().Sub(start)
// 	ok := (err == nil)
// 	w.info("TX:%s Sent. OK=%t [%.2fs]", txHash, ok, callTime.Seconds())
// 	return txHash, err
// }

// type sendTxArgs struct {
// 	Nonce    hexutil.Uint64 `json:"nonce"`
// 	From     string         `json:"from"`
// 	To       string         `json:"to,omitempty"`
// 	Gas      hexutil.Uint64 `json:"gas"`
// 	GasPrice hexutil.Big    `json:"gasPrice"`
// 	Value    hexutil.Big    `json:"value"`
// 	Data     *hexutil.Bytes `json:"data"`
// 	// EEA spec extensions
// 	PrivateFrom string   `json:"privateFrom,omitempty"`
// 	PrivateFor  []string `json:"privateFor,omitempty"`
// }

// // sendUnsignedTxn sends a transaction for internal signing by the node
// func (e *Eth) sendUnsignedTxn(ctx context.Context, tx *types.Transaction) (string, error) {
// 	data := hexutil.Bytes(tx.Data())
// 	args := sendTxArgs{
// 		Nonce:    hexutil.Uint64(w.Nonce),
// 		From:     w.Account.Hex(),
// 		Gas:      hexutil.Uint64(tx.Gas()),
// 		GasPrice: hexutil.Big(*tx.GasPrice()),
// 		Value:    hexutil.Big(*tx.Value()),
// 		Data:     &data,
// 	}
// 	if w.Exerciser.PrivateFrom != "" {
// 		args.PrivateFrom = w.Exerciser.PrivateFrom
// 		args.PrivateFor = w.Exerciser.PrivateFor
// 	}
// 	var to = tx.To()
// 	if to != nil {
// 		args.To = to.Hex()
// 	}
// 	var txHash string
// 	err := w.RPC.CallContext(ctx, &txHash, "eth_sendTransaction", args)
// 	return txHash, err
// }

// // callContract call a transaction and return the result as a string
// func (e *Eth) callContract(tx *types.Transaction) (err error) {

// 	start := time.Now()

// 	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
// 	defer cancel()

// 	data := hexutil.Bytes(tx.Data())
// 	args := sendTxArgs{
// 		Nonce:    hexutil.Uint64(w.Nonce),
// 		From:     w.Account.Hex(),
// 		To:       tx.To().Hex(),
// 		Gas:      hexutil.Uint64(tx.Gas()),
// 		GasPrice: hexutil.Big(*tx.GasPrice()),
// 		Value:    hexutil.Big(*tx.Value()),
// 		Data:     &data,
// 	}

// 	if w.Exerciser.EstimateGas {
// 		var retValue hexutil.Uint64
// 		err = w.RPC.CallContext(ctx, &retValue, "eth_estimateGas", args)
// 		callTime := time.Now().Sub(start)
// 		w.info("Estimate Gas result: %d [%.2fs]", retValue, callTime.Seconds())
// 	} else {
// 		var retValue string
// 		err = w.RPC.CallContext(ctx, &retValue, "eth_call", args, "latest")
// 		callTime := time.Now().Sub(start)
// 		w.info("Call result: '%s' [%.2fs]", retValue, callTime.Seconds())
// 	}
// 	if err != nil {
// 		return fmt.Errorf("Contract call failed: %s", err)
// 	}

// 	return
// }

// // signAndSendTxn externally signs and sends a transaction
// func (e *Eth) signAndSendTxn(ctx context.Context, tx *types.Transaction) (string, error) {
// 	signedTx, _ := types.SignTx(tx, w.Signer, w.PrivateKey)
// 	var buff bytes.Buffer
// 	signedTx.EncodeRLP(&buff)
// 	from, _ := types.Sender(w.Signer, signedTx)
// 	log.Debug("TX signed. ChainID=%d From=%s", w.Exerciser.ChainID, from.Hex())

// 	var txHash string
// 	data, err := rlp.EncodeToBytes(signedTx)
// 	if err != nil {
// 		return txHash, fmt.Errorf("Failed to RLP encode: %s", err)
// 	}
// 	err = w.RPC.CallContext(ctx, &txHash, "eth_sendRawTransaction", common.ToHex(data))
// 	return txHash, err
// }

// type txnReceipt struct {
// 	BlockHash         *common.Hash    `json:"blockHash"`
// 	BlockNumber       *hexutil.Big    `json:"blockNumber"`
// 	ContractAddress   *common.Address `json:"contractAddress"`
// 	CumulativeGasUsed *hexutil.Big    `json:"cumulativeGasUsed"`
// 	TransactionHash   *common.Hash    `json:"transactionHash"`
// 	From              *common.Address `json:"from"`
// 	GasUsed           *hexutil.Big    `json:"gasUsed"`
// 	Status            *hexutil.Big    `json:"status"`
// 	To                *common.Address `json:"to"`
// 	TransactionIndex  *hexutil.Uint   `json:"transactionIndex"`
// }

// // WaitUntilMined waits until a given transaction has been mined
// func (e *Eth) waitUntilMined(start time.Time, txHash string) (*txnReceipt, error) {

// 	var isMined = false

// 	// After this initial sleep we wait up to a maximum time,
// 	// checking periodically - 5 times up to the maximum
// 	retryDelay := time.Duration(float64(w.Exerciser.ReceiptWaitMax-w.Exerciser.ReceiptWaitMin)/5) * time.Second

// 	var receipt txnReceipt
// 	for !isMined {
// 		callStart := time.Now()
// 		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
// 		defer cancel()

// 		err := w.RPC.CallContext(ctx, &receipt, "eth_getTransactionReceipt", common.HexToHash(txHash))
// 		elapsed := time.Now().Sub(start)
// 		callTime := time.Now().Sub(callStart)

// 		isMined = receipt.BlockNumber != nil && receipt.BlockNumber.ToInt().Uint64() > 0
// 		w.info("TX:%s Mined=%t after %.2fs [%.2fs]", txHash, isMined, elapsed.Seconds(), callTime.Seconds())
// 		if err != nil && err != ethereum.NotFound {
// 			return nil, fmt.Errorf("Requesting TX receipt: %s", err)
// 		}
// 		if receipt.Status != nil {
// 			log.Debug("Status=%s BlockNumber=%s BlockHash=%x TransactionIndex=%d GasUsed=%s CumulativeGasUsed=%s",
// 				receipt.Status.ToInt(), receipt.BlockNumber.ToInt(), receipt.BlockHash,
// 				receipt.TransactionIndex, receipt.GasUsed.ToInt(), receipt.CumulativeGasUsed.ToInt())
// 		}
// 		if !isMined && elapsed > time.Duration(w.Exerciser.ReceiptWaitMax)*time.Second {
// 			return nil, fmt.Errorf("Timed out waiting for TX receipt after %.2fs", elapsed.Seconds())
// 		}
// 		if !isMined {
// 			time.Sleep(retryDelay)
// 		}
// 	}

// 	return &receipt, nil
// }

// // SendAndWaitForMining sends a single transaction and waits for it to be mined
// func (e *Eth) sendAndWaitForMining(tx *types.Transaction) (*txnReceipt, error) {
// 	txHash, err := w.sendTransaction(tx)
// 	var receipt *txnReceipt
// 	if err != nil {
// 		w.error("failed sending TX: %s", err)
// 	} else {
// 		w.Nonce++
// 		// Wait for mining
// 		start := time.Now()
// 		log.Debug("Waiting for %d seconds for tx be mined in next block", w.Exerciser.ReceiptWaitMin)
// 		time.Sleep(time.Duration(w.Exerciser.ReceiptWaitMin) * time.Second)
// 		receipt, err = w.waitUntilMined(start, txHash)
// 		if err != nil {
// 			return nil, fmt.Errorf("failed checking TX receipt: %s", err)
// 		}
// 	}
// 	return receipt, err
// }

// // initializeNonce get the initial nonce to use
// func (e *Eth) initializeNonce(address string) error {
// 	var result hexutil.Uint64
// 	err := w.RPC.Call(&result, "eth_getTransactionCount", address, "latest")
// 	if err != nil {
// 		return fmt.Errorf("Failed to get transaction count for %s: %s", address, err)
// 	}
// 	w.Nonce = uint64(result)
// 	if err != nil {
// 		return fmt.Errorf("Failed to parse transaction count '%s' for %s: %s", result, address, err)
// 	}
// 	return nil
// }

// // GetNetworkID returns the network ID from the node
// func (e *Eth) GetNetworkID() (int64, error) {
// 	var strNetworkID string
// 	err := w.RPC.Call(&strNetworkID, "net_version")
// 	if err != nil {
// 		return 0, fmt.Errorf("Failed to query network ID (to use as chain ID in EIP155 signing): %s", err)
// 	}
// 	networkID, err := strconv.ParseInt(strNetworkID, 10, 64)
// 	if err != nil {
// 		return 0, fmt.Errorf("Failed to parse network ID returned from node '%s': %s", strNetworkID, err)
// 	}
// 	return networkID, nil
// }

// // Init the account and connection for this worker
// func (e *Eth) Init() error {

// 	// Connect the client
// 	rpc, err := rpc.Dial(w.Exerciser.URL)
// 	if err != nil {
// 		return fmt.Errorf("Connect to %s failed: %s", w.Exerciser.URL, err)
// 	}
// 	w.RPC = rpc
// 	log.Debug(w.Name, ": connected. URL=", w.Exerciser.URL)

// 	// Generate or allocate an account from the exerciser
// 	if w.Exerciser.ExternalSign {
// 		if err := w.generateAccount(); err != nil {
// 			return err
// 		}
// 	} else {
// 		account := w.Exerciser.Accounts[w.Index]
// 		if !common.IsHexAddress(account) {
// 			return fmt.Errorf("Invalid account address (20 hex bytes with '0x' prefix): %s", account)
// 		}
// 		w.Account = common.HexToAddress(account)

// 		// Get the initial nonce for this existing account
// 		if err := w.initializeNonce(account); err != nil {
// 			return err
// 		}
// 	}

// 	return nil
// }

// // InstallContract installs the contract and returns the address
// func (e *Eth) InstallContract() (*common.Address, error) {
// 	tx := types.NewContractCreation(
// 		w.Nonce,
// 		big.NewInt(w.Exerciser.Amount),
// 		uint64(w.Exerciser.Gas),
// 		big.NewInt(w.Exerciser.GasPrice),
// 		common.FromHex(w.CompiledContract.Compiled),
// 	)
// 	receipt, err := w.sendAndWaitForMining(tx)
// 	if err != nil {
// 		return nil, fmt.Errorf("Failed to install contract: %s", err)
// 	}
// 	return receipt.ContractAddress, nil
// }

// // CallOnce executes a contract once and returns
// func (e *Eth) CallOnce() error {
// 	tx := w.generateTransaction()
// 	err := w.callContract(tx)
// 	return err
// }

// // Run executes the specified exerciser workload then exits
// func (e *Eth) Run() {
// 	log.Debug(w.Name, ": started. ", w.Exerciser.TxnsPerLoop, " tx/loop for ", w.Exerciser.Loops, " loops. Account=", w.Account.Hex())

// 	var successes, failures uint64
// 	infinite := (w.Exerciser.Loops == 0)
// 	for ; w.LoopIndex < uint64(w.Exerciser.Loops) || infinite; w.LoopIndex++ {

// 		// Send a set of transactions before waiting for receipts (which takes some time)
// 		var txHashes []string
// 		for i := 0; i < w.Exerciser.TxnsPerLoop; i++ {
// 			tx := w.generateTransaction()
// 			txHash, err := w.sendTransaction(tx)
// 			if err != nil {
// 				w.error("TX send failed (%d/%d): %s", i, w.Exerciser.TxnsPerLoop, err)
// 			} else {
// 				w.Nonce++
// 				txHashes = append(txHashes, txHash)
// 			}
// 		}

// 		// Transactions will not be mined immediately.
// 		// Wait for number of configurable number of seconds before attempting
// 		// to check for the transaction receipt.
// 		// ** This should be greater than the block period **
// 		log.Debug("Waiting for %d seconds for tx be mined in next block", w.Exerciser.ReceiptWaitMin)
// 		start := time.Now()
// 		time.Sleep(time.Duration(w.Exerciser.ReceiptWaitMin) * time.Second)

// 		// Wait the receipts of all successfully set transctions
// 		var loopSuccesses uint64
// 		for _, txHash := range txHashes {
// 			_, err := w.waitUntilMined(start, txHash)
// 			if err != nil {
// 				w.error("TX:%s failed checking receipt: %s", txHash, err)
// 			} else {
// 				loopSuccesses++
// 			}
// 		}
// 		var loopFailures = uint64(w.Exerciser.TxnsPerLoop) - loopSuccesses

// 		// Increment global counters
// 		successes += loopSuccesses
// 		atomic.AddUint64(&w.Exerciser.TotalSuccesses, loopSuccesses)
// 		failures += loopFailures
// 		atomic.AddUint64(&w.Exerciser.TotalFailures, loopFailures)
// 	}

// 	w.RPC.Close()
// 	log.Debug(w.Name, ": finished. Loops=", w.LoopIndex, " Success=", successes, " Failures=", failures)
// }
