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

package kldcontracts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
)

// REST2EthAsyncDispatcher is passed in to process messages over a streaming system with
// a receipt store. Only used for POST methods, when kld-sync is not set to true
type REST2EthAsyncDispatcher interface {
	DispatchMsgAsync(msg map[string]interface{}, ack bool) (*kldmessages.AsyncSentMsg, error)
}

// rest2EthSyncDispatcher abstracts the processing of the transactions and queries
// synchronously. We perform those within this package.
type rest2EthSyncDispatcher interface {
	DispatchSendTransactionSync(msg *kldmessages.SendTransaction, replyProcessor rest2EthReplyProcessor)
	DispatchDeployContractSync(msg *kldmessages.DeployContract, replyProcessor rest2EthReplyProcessor)
}

// rest2EthReplyProcessor interface
type rest2EthReplyProcessor interface {
	ReplyWithError(err error)
	ReplyWithReceipt(receipt kldmessages.ReplyWithHeaders)
}

// rest2eth provides the HTTP <-> kldmessages translation and dispatches for processing
type rest2eth struct {
	gw              smartContractGatewayInt
	addrCheck       *regexp.Regexp
	rpc             kldeth.RPCClient
	asyncDispatcher REST2EthAsyncDispatcher
	syncDispatcher  rest2EthSyncDispatcher
}

type restErrMsg struct {
	Message string `json:"error"`
}

type restAsyncMsg struct {
	OK string `json:"ok"`
}

// rest2EthInflight is instantiated for each async reply in flight
type rest2EthSyncResponder struct {
	r      *rest2eth
	res    http.ResponseWriter
	req    *http.Request
	done   bool
	waiter *sync.Cond
}

func (i *rest2EthSyncResponder) ReplyWithError(err error) {
	i.r.restErrReply(i.res, i.req, err, 500)
	i.done = true
	i.waiter.Broadcast()
	return
}

func (i *rest2EthSyncResponder) ReplyWithReceipt(receipt kldmessages.ReplyWithHeaders) {
	txReceiptMsg := receipt.IsReceipt()
	if txReceiptMsg != nil && txReceiptMsg.ContractAddress != nil {
		i.r.gw.PostDeploy(txReceiptMsg)
	}
	status := 200
	reply, _ := json.MarshalIndent(receipt, "", "  ")
	log.Infof("<-- %s %s [%d]", i.req.Method, i.req.URL, status)
	log.Debugf("<-- %s", reply)
	i.res.WriteHeader(status)
	i.res.Header().Set("Content-Type", "application/json")
	i.res.Write(reply)
	i.done = true
	i.waiter.Broadcast()
	return
}

func newREST2eth(gw smartContractGatewayInt, rpc kldeth.RPCClient, asyncDispatcher REST2EthAsyncDispatcher, syncDispatcher rest2EthSyncDispatcher) *rest2eth {
	addrCheck, _ := regexp.Compile("^(0x)?[0-9a-z]{40}$")
	return &rest2eth{
		gw:              gw,
		addrCheck:       addrCheck,
		syncDispatcher:  syncDispatcher,
		asyncDispatcher: asyncDispatcher,
		rpc:             rpc,
	}
}

func (r *rest2eth) addRoutes(router *httprouter.Router) {
	router.POST("/contracts/:address/:method", r.restHandler)
	router.GET("/contracts/:address/:method", r.restHandler)
	router.POST("/abis/:abi", r.restHandler)
	router.POST("/abis/:abi/:address/:method", r.restHandler)
	router.GET("/abis/:abi/:address/:method", r.restHandler)
}

// getKLDParam standardizes how special 'kld' params are specified, in query params, or headers
func (r *rest2eth) getKLDParam(name string, req *http.Request, isBool bool) string {
	val := req.FormValue("kld-" + name)
	if val == "" && isBool {
		// If the user specified an empty query field, treat that as true
		if vs := req.Form["kld-"+name]; len(vs) > 0 {
			val = "true"
		}
	}
	if val == "" {
		val = req.Header.Get("x-kaleido-" + name)
	}
	return val
}

func (r *rest2eth) resolveParams(res http.ResponseWriter, req *http.Request, params httprouter.Params) (from, addr string, value json.Number, abiMethod *abi.Method, deployMsg *kldmessages.DeployContract, msgParams []interface{}, err error) {
	addr = strings.ToLower(strings.TrimPrefix(params.ByName("address"), "0x"))

	from = strings.ToLower(strings.TrimPrefix(r.getKLDParam("from", req, false), "0x"))
	if from != "" && !r.addrCheck.MatchString(from) {
		log.Errorf("Invalid from address: '%s' (original input = '%s')", from, req.FormValue("from"))
		err = fmt.Errorf("From Address must be a 40 character hex string (0x prefix is optional)")
		r.restErrReply(res, req, err, 404)
		return
	}

	value = json.Number(r.getKLDParam("value", req, false))

	var a *kldbind.ABI
	abiID := params.ByName("abi")
	if abiID != "" {
		deployMsg, err = r.gw.loadDeployMsgForFactory(abiID)
		if err != nil {
			r.restErrReply(res, req, err, 404)
			return
		}
		a = deployMsg.ABI
	} else {
		a, err = r.gw.loadABIForInstance(addr)
		if err != nil {
			r.restErrReply(res, req, err, 404)
			return
		}
	}

	methodName := params.ByName("method")
	if methodName == "" {
		abiMethod = &a.ABI.Constructor
	} else {
		if !r.addrCheck.MatchString(addr) {
			log.Errorf("Invalid to addres: '%s' (original input = '%s')", addr, params.ByName("address"))
			err = fmt.Errorf("To Address must be a 40 character hex string (0x prefix is optional)")
			r.restErrReply(res, req, err, 404)
			return
		}

		for _, method := range a.ABI.Methods {
			if method.Name == methodName {
				abiMethod = &method
				break
			}
		}
		if abiMethod == nil {
			err = fmt.Errorf("Method '%s' is not declared in the ABI of contract '%s'", url.QueryEscape(methodName), addr)
			r.restErrReply(res, req, err, 404)
			return
		}
	}

	body, err := kldutils.YAMLorJSONPayload(req)
	if err != nil {
		r.restErrReply(res, req, err, 400)
		return
	}

	msgParams = make([]interface{}, 0, len(abiMethod.Inputs))
	queryParams := req.Form
	for _, abiParam := range abiMethod.Inputs {
		// Body takes precedence
		msgParam := make(map[string]interface{})
		msgParam["type"] = abiParam.Type.String()
		if bv, exists := body[abiParam.Name]; exists {
			msgParam["value"] = bv
		} else if vs := queryParams[abiParam.Name]; len(vs) > 0 {
			msgParam["value"] = vs[0]
		} else {
			err = fmt.Errorf("Parameter '%s' of method '%s' was not specified in body or query parameters", abiParam.Name, abiMethod.Name)
			r.restErrReply(res, req, err, 404)
			return
		}
		msgParams = append(msgParams, msgParam)
	}
	return
}

func (r *rest2eth) restHandler(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	from, addr, value, abiMethod, deployMsg, msgParams, err := r.resolveParams(res, req, params)
	if err != nil {
		return
	}

	if (!abiMethod.Const) &&
		strings.ToLower(r.getKLDParam("call", req, true)) != "true" {
		if from == "" {
			err = fmt.Errorf("Please specify a valid address in the 'kld-from' query string parameter or x-kaleido-from HTTP header")
			r.restErrReply(res, req, err, 400)
			return
		}
		if deployMsg != nil {
			r.deployContract(res, req, from, value, abiMethod, deployMsg, msgParams)
		} else {
			r.sendTransaction(res, req, from, addr, value, abiMethod, msgParams)
		}
	} else {
		r.callContract(res, req, from, addr, value, abiMethod, msgParams)
	}
}

func (r *rest2eth) deployContract(res http.ResponseWriter, req *http.Request, from string, value json.Number, abiMethod *abi.Method, deployMsg *kldmessages.DeployContract, msgParams []interface{}) {

	deployMsg.Headers.MsgType = kldmessages.MsgTypeDeployContract
	deployMsg.From = "0x" + from
	deployMsg.Gas = json.Number(r.getKLDParam("gas", req, false))
	deployMsg.GasPrice = json.Number(r.getKLDParam("gasprice", req, false))
	deployMsg.Value = value
	deployMsg.Parameters = msgParams
	if strings.ToLower(r.getKLDParam("sync", req, true)) == "true" {
		responder := &rest2EthSyncResponder{
			r:      r,
			res:    res,
			req:    req,
			done:   false,
			waiter: sync.NewCond(&sync.Mutex{}),
		}
		r.syncDispatcher.DispatchDeployContractSync(deployMsg, responder)
		responder.waiter.L.Lock()
		for !responder.done {
			responder.waiter.Wait()
		}
	} else {
		ack := (r.getKLDParam("noack", req, true) != "true") // turn on ack's by default

		// Async messages are dispatched as generic map payloads.
		// We are confident in the re-serialization here as we've deserialized from JSON then built our own structure
		msgBytes, _ := json.Marshal(deployMsg)
		var mapMsg map[string]interface{}
		json.Unmarshal(msgBytes, &mapMsg)
		if asyncResponse, err := r.asyncDispatcher.DispatchMsgAsync(mapMsg, ack); err != nil {
			r.restErrReply(res, req, err, 500)
		} else {
			r.restAsyncReply(res, req, asyncResponse)
		}
	}
	return
}

func (r *rest2eth) sendTransaction(res http.ResponseWriter, req *http.Request, from, addr string, value json.Number, abiMethod *abi.Method, msgParams []interface{}) {

	msg := &kldmessages.SendTransaction{}
	msg.Headers.MsgType = kldmessages.MsgTypeSendTransaction
	msg.MethodName = abiMethod.Name
	msg.To = "0x" + addr
	msg.From = "0x" + from
	msg.Gas = json.Number(r.getKLDParam("gas", req, false))
	msg.GasPrice = json.Number(r.getKLDParam("gasprice", req, false))
	msg.Value = value
	msg.Parameters = msgParams

	if strings.ToLower(r.getKLDParam("sync", req, true)) == "true" {
		responder := &rest2EthSyncResponder{
			r:      r,
			res:    res,
			req:    req,
			done:   false,
			waiter: sync.NewCond(&sync.Mutex{}),
		}
		r.syncDispatcher.DispatchSendTransactionSync(msg, responder)
		responder.waiter.L.Lock()
		for !responder.done {
			responder.waiter.Wait()
		}
	} else {
		ack := (r.getKLDParam("noack", req, true) != "true") // turn on ack's by default

		// Async messages are dispatched as generic map payloads.
		// We are confident in the re-serialization here as we've deserialized from JSON then built our own structure
		msgBytes, _ := json.Marshal(msg)
		var mapMsg map[string]interface{}
		json.Unmarshal(msgBytes, &mapMsg)
		if asyncResponse, err := r.asyncDispatcher.DispatchMsgAsync(mapMsg, ack); err != nil {
			r.restErrReply(res, req, err, 500)
		} else {
			r.restAsyncReply(res, req, asyncResponse)
		}
	}
	return
}

func (r *rest2eth) callContract(res http.ResponseWriter, req *http.Request, from, addr string, value json.Number, abiMethod *abi.Method, msgParams []interface{}) {
	resBody, err := kldeth.CallMethod(r.rpc, from, addr, value, abiMethod, msgParams)
	if err != nil {
		r.restErrReply(res, req, err, 500)
		return
	}
	resBytes, _ := json.MarshalIndent(&resBody, "", "  ")
	status := 200
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	log.Debugf("<-- %s", resBytes)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	res.Write(resBytes)
	return
}

func (r *rest2eth) restAsyncReply(res http.ResponseWriter, req *http.Request, asyncResponse *kldmessages.AsyncSentMsg) {
	resBytes, _ := json.Marshal(asyncResponse)
	status := 202 // accepted
	log.Infof("<-- %s %s [%d]:\n%s", req.Method, req.URL, status, string(resBytes))
	log.Debugf("<-- %s", resBytes)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	res.Write(resBytes)
}

func (r *rest2eth) restErrReply(res http.ResponseWriter, req *http.Request, err error, status int) {
	log.Errorf("<-- %s %s [%d]: %s", req.Method, req.URL, status, err)
	reply, _ := json.Marshal(&restErrMsg{Message: err.Error()})
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	res.Write(reply)
	return
}
