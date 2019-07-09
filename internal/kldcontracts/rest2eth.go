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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldevents"
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
	subMgr          kldevents.SubscriptionManager
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
	i.res.Header().Set("Content-Type", "application/json")
	i.res.WriteHeader(status)
	i.res.Write(reply)
	i.done = true
	i.waiter.Broadcast()
	return
}

func newREST2eth(gw smartContractGatewayInt, rpc kldeth.RPCClient, subMgr kldevents.SubscriptionManager, asyncDispatcher REST2EthAsyncDispatcher, syncDispatcher rest2EthSyncDispatcher) *rest2eth {
	addrCheck, _ := regexp.Compile("^(0x)?[0-9a-z]{40}$")
	return &rest2eth{
		gw:              gw,
		addrCheck:       addrCheck,
		syncDispatcher:  syncDispatcher,
		asyncDispatcher: asyncDispatcher,
		rpc:             rpc,
		subMgr:          subMgr,
	}
}

func (r *rest2eth) addRoutes(router *httprouter.Router) {
	router.POST("/contracts/:address/:method", r.restHandler)
	router.GET("/contracts/:address/:method", r.restHandler)
	router.POST("/contracts/:address/:method/:subcommand", r.restHandler)
	router.POST("/abis/:abi", r.restHandler)
	router.POST("/abis/:abi/:address/:method", r.restHandler)
	router.GET("/abis/:abi/:address/:method", r.restHandler)
	router.GET("/abis/:abi/:address/:method/:subcommand", r.restHandler)
}

type restCmd struct {
	from      string
	addr      string
	value     json.Number
	abiMethod *abi.Method
	abiEvent  *abi.Event
	deployMsg *kldmessages.DeployContract
	body      map[string]interface{}
	msgParams []interface{}
}

func (r *rest2eth) resolveParams(res http.ResponseWriter, req *http.Request, params httprouter.Params) (c restCmd, err error) {

	// Check if we have a valid address in :address (verified later if required)
	addrParam := params.ByName("address")
	c.addr = strings.ToLower(strings.TrimPrefix(addrParam, "0x"))
	validAddress := r.addrCheck.MatchString(c.addr)

	// Check we have a valid ABI
	var a *kldbind.ABI
	abiID := params.ByName("abi")
	if abiID != "" {
		c.deployMsg, err = r.gw.loadDeployMsgForFactory(abiID)
		if err != nil {
			r.restErrReply(res, req, err, 404)
			return
		}
		a = c.deployMsg.ABI
	} else {
		if !validAddress {
			// Resolve the address as a registered name, to an actual contract address
			if c.addr, err = r.gw.resolveContractAddr(addrParam); err != nil {
				r.restErrReply(res, req, err, 404)
				return
			}
			validAddress = true
		}
		a, err = r.gw.loadABIForInstance(c.addr)
		if err != nil {
			r.restErrReply(res, req, err, 404)
			return
		}
	}

	// See addRoutes for all the various routes we support.
	// We need to handle the special case of
	// /abis/:abi/EVENTNAME/subscribe
	// ... where 'EVENTNAME' is passed as :address and is a valid event
	// and where 'subscribe' is passed as :method

	// Check if we have a method in :method param
	methodParam := params.ByName("method")
	methodParamLC := strings.ToLower(methodParam)
	if methodParam != "" {
		for _, method := range a.ABI.Methods {
			if method.Name == methodParam {
				c.abiMethod = &method
				break
			}
		}
	}

	// Then if we don't have a method in :method param, we might have
	// an event in either the :event OR :address param (see special case above)
	// Note solidity guarantees no overlap in method / event names
	if c.abiMethod == nil && methodParam != "" {
		for _, event := range a.ABI.Events {
			if event.Name == methodParam {
				c.abiEvent = &event
				break
			}
			if methodParamLC == "subscribe" && event.Name == addrParam {
				c.addr = ""
				c.abiEvent = &event
				break
			}
		}
	}

	// Last case is the constructor, where nothing is specified
	if methodParam == "" && c.abiMethod == nil && c.abiEvent == nil {
		c.abiMethod = &a.ABI.Constructor
	}

	// If we didn't find the method or event, report to the user
	if c.abiMethod == nil && c.abiEvent == nil {
		if methodParamLC == "subscribe" {
			err = fmt.Errorf("Event '%s' is not declared in the ABI", methodParam)
			r.restErrReply(res, req, err, 404)
			return
		}
		err = fmt.Errorf("Method or Event '%s' is not declared in the ABI of contract '%s'", url.QueryEscape(methodParam), c.addr)
		r.restErrReply(res, req, err, 404)
		return
	}

	// If we have an address, it must be valid
	if c.addr != "" && !validAddress {
		log.Errorf("Invalid to addres: '%s' (original input = '%s')", c.addr, params.ByName("address"))
		err = fmt.Errorf("To Address must be a 40 character hex string (0x prefix is optional)")
		r.restErrReply(res, req, err, 404)
		return
	}

	// If we have a from, it needs to be a valid address
	c.from = strings.ToLower(strings.TrimPrefix(getKLDParam("from", req, false), "0x"))
	if c.from != "" && !r.addrCheck.MatchString(c.from) {
		log.Errorf("Invalid from address: '%s' (original input = '%s')", c.from, req.FormValue("from"))
		err = fmt.Errorf("From Address must be a 40 character hex string (0x prefix is optional)")
		r.restErrReply(res, req, err, 404)
		return
	}
	c.value = json.Number(getKLDParam("ethvalue", req, false))

	c.body, err = kldutils.YAMLorJSONPayload(req)
	if err != nil {
		r.restErrReply(res, req, err, 400)
		return
	}

	if c.abiEvent != nil {
		return
	}

	c.msgParams = make([]interface{}, 0, len(c.abiMethod.Inputs))
	queryParams := req.Form
	for _, abiParam := range c.abiMethod.Inputs {
		// Body takes precedence
		msgParam := make(map[string]interface{})
		msgParam["type"] = abiParam.Type.String()
		if bv, exists := c.body[abiParam.Name]; exists {
			msgParam["value"] = bv
		} else if vs := queryParams[abiParam.Name]; len(vs) > 0 {
			msgParam["value"] = vs[0]
		} else {
			err = fmt.Errorf("Parameter '%s' of method '%s' was not specified in body or query parameters", abiParam.Name, c.abiMethod.Name)
			r.restErrReply(res, req, err, 400)
			return
		}
		c.msgParams = append(c.msgParams, msgParam)
	}
	return
}

func (r *rest2eth) restHandler(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	c, err := r.resolveParams(res, req, params)
	if err != nil {
		return
	}

	if c.abiEvent != nil {
		r.subscribeEvent(res, req, c.addr, c.abiEvent, c.body)
	} else if (!c.abiMethod.Const) && strings.ToLower(getKLDParam("call", req, true)) != "true" {
		if c.from == "" {
			err = fmt.Errorf("Please specify a valid address in the 'kld-from' query string parameter or x-kaleido-from HTTP header")
			r.restErrReply(res, req, err, 400)
		} else if c.deployMsg != nil {
			r.deployContract(res, req, c.from, c.value, c.abiMethod, c.deployMsg, c.msgParams)
		} else {
			r.sendTransaction(res, req, c.from, c.addr, c.value, c.abiMethod, c.msgParams)
		}
	} else {
		r.callContract(res, req, c.from, c.addr, c.value, c.abiMethod, c.msgParams)
	}
}

func (r *rest2eth) fromBodyOrForm(req *http.Request, body map[string]interface{}, param string) string {
	val := body["stream"]
	valType := reflect.TypeOf(val)
	if valType != nil && valType.Kind() == reflect.String && len(val.(string)) > 0 {
		return val.(string)
	}
	return req.FormValue("stream")
}

func (r *rest2eth) subscribeEvent(res http.ResponseWriter, req *http.Request, addrStr string, abiEvent *abi.Event, body map[string]interface{}) {
	if r.subMgr == nil {
		r.restErrReply(res, req, errors.New(errEventSupportMissing), 405)
		return
	}
	streamID := r.fromBodyOrForm(req, body, "stream")
	if streamID == "" {
		r.restErrReply(res, req, fmt.Errorf("Must supply a 'stream' parameter in the body or query"), 400)
		return
	}
	var addr *common.Address
	if addrStr != "" {
		address := common.HexToAddress("0x" + addrStr)
		addr = &address
	}
	sub, err := r.subMgr.AddSubscription(addr, abiEvent, streamID)
	if err != nil {
		r.restErrReply(res, req, err, 400)
		return
	}
	status := 200
	resBytes, _ := json.Marshal(sub)
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	log.Debugf("<-- %s", resBytes)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	res.Write(resBytes)
}

func (r *rest2eth) doubleURLDecode(s string) string {
	// Due to an annoying bug in the rapidoc Swagger UI, it is double URL encoding parameters.
	// As most constellation b64 encoded values end in "=" that's breaking the ability to use
	// the UI. As they do not contain a % we just double URL decode them :-(
	// However, this translates '+' into ' ' (space), so we have to fix that too.
	doubleDecoded, _ := url.QueryUnescape(s)
	return strings.ReplaceAll(doubleDecoded, " ", "+")
}

func (r *rest2eth) addPrivateTx(msg *kldmessages.TransactionCommon, req *http.Request) {
	msg.PrivateFrom = r.doubleURLDecode(getKLDParam("privatefrom", req, false))
	msg.PrivateFor = getKLDParamMulti("privatefor", req)
	for idx, val := range msg.PrivateFor {
		msg.PrivateFor[idx] = r.doubleURLDecode(val)
	}
}

func (r *rest2eth) deployContract(res http.ResponseWriter, req *http.Request, from string, value json.Number, abiMethod *abi.Method, deployMsg *kldmessages.DeployContract, msgParams []interface{}) {

	deployMsg.Headers.MsgType = kldmessages.MsgTypeDeployContract
	deployMsg.From = "0x" + from
	deployMsg.Gas = json.Number(getKLDParam("gas", req, false))
	deployMsg.GasPrice = json.Number(getKLDParam("gasprice", req, false))
	deployMsg.Value = value
	deployMsg.Parameters = msgParams
	r.addPrivateTx(&deployMsg.TransactionCommon, req)
	deployMsg.RegisterAs = getKLDParam("register", req, false)
	if err := r.gw.checkNameAvailable(deployMsg.RegisterAs); err != nil {
		r.restErrReply(res, req, err, 409)
		return
	}
	if strings.ToLower(getKLDParam("sync", req, true)) == "true" {
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
		ack := (getKLDParam("noack", req, true) != "true") // turn on ack's by default

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
	msg.Gas = json.Number(getKLDParam("gas", req, false))
	msg.GasPrice = json.Number(getKLDParam("gasprice", req, false))
	msg.Value = value
	msg.Parameters = msgParams
	r.addPrivateTx(&msg.TransactionCommon, req)

	if strings.ToLower(getKLDParam("sync", req, true)) == "true" {
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
		ack := (getKLDParam("noack", req, true) != "true") // turn on ack's by default

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
