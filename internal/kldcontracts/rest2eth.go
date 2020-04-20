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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldauth"
	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldevents"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldtx"
	"github.com/kaleido-io/ethconnect/internal/kldutils"

	log "github.com/sirupsen/logrus"
)

// REST2EthAsyncDispatcher is passed in to process messages over a streaming system with
// a receipt store. Only used for POST methods, when kld-sync is not set to true
type REST2EthAsyncDispatcher interface {
	DispatchMsgAsync(ctx context.Context, msg map[string]interface{}, ack bool) (*kldmessages.AsyncSentMsg, error)
}

// rest2EthSyncDispatcher abstracts the processing of the transactions and queries
// synchronously. We perform those within this package.
type rest2EthSyncDispatcher interface {
	DispatchSendTransactionSync(ctx context.Context, msg *kldmessages.SendTransaction, replyProcessor rest2EthReplyProcessor)
	DispatchDeployContractSync(ctx context.Context, msg *kldmessages.DeployContract, replyProcessor rest2EthReplyProcessor)
}

// rest2EthReplyProcessor interface
type rest2EthReplyProcessor interface {
	ReplyWithError(err error)
	ReplyWithReceipt(receipt kldmessages.ReplyWithHeaders)
	ReplyWithReceiptAndError(receipt kldmessages.ReplyWithHeaders, err error)
}

// rest2eth provides the HTTP <-> kldmessages translation and dispatches for processing
type rest2eth struct {
	gw              smartContractGatewayInt
	rpc             kldeth.RPCClient
	asyncDispatcher REST2EthAsyncDispatcher
	syncDispatcher  rest2EthSyncDispatcher
	subMgr          kldevents.SubscriptionManager
	rr              RemoteRegistry
}

type restErrMsg struct {
	Message string `json:"error"`
}

type restAsyncMsg struct {
	OK string `json:"ok"`
}

type restReceiptAndError struct {
	Message string `json:"error"`
	kldmessages.ReplyWithHeaders
}

// rest2EthInflight is instantiated for each async reply in flight
type rest2EthSyncResponder struct {
	r      *rest2eth
	res    http.ResponseWriter
	req    *http.Request
	done   bool
	waiter *sync.Cond
}

var addrCheck = regexp.MustCompile("^(0x)?[0-9a-z]{40}$")

func (i *rest2EthSyncResponder) ReplyWithError(err error) {
	i.r.restErrReply(i.res, i.req, err, 500)
	i.done = true
	i.waiter.Broadcast()
	return
}

func (i *rest2EthSyncResponder) ReplyWithReceiptAndError(receipt kldmessages.ReplyWithHeaders, err error) {
	status := 500
	reply, _ := json.MarshalIndent(&restReceiptAndError{err.Error(), receipt}, "", "  ")
	log.Infof("<-- %s %s [%d]", i.req.Method, i.req.URL, status)
	log.Debugf("<-- %s", reply)
	i.res.Header().Set("Content-Type", "application/json")
	i.res.WriteHeader(status)
	i.res.Write(reply)
	i.done = true
	i.waiter.Broadcast()
	return
}

func (i *rest2EthSyncResponder) ReplyWithReceipt(receipt kldmessages.ReplyWithHeaders) {
	txReceiptMsg := receipt.IsReceipt()
	if txReceiptMsg != nil && txReceiptMsg.ContractAddress != nil {
		if err := i.r.gw.PostDeploy(txReceiptMsg); err != nil {
			log.Warnf("Failed to perform post-deploy processing: %s", err)
			i.ReplyWithReceiptAndError(receipt, err)
			return
		}
	}
	status := 200
	if receipt.ReplyHeaders().MsgType != kldmessages.MsgTypeTransactionSuccess {
		status = 500
	}
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

func newREST2eth(gw smartContractGatewayInt, rpc kldeth.RPCClient, subMgr kldevents.SubscriptionManager, rr RemoteRegistry, asyncDispatcher REST2EthAsyncDispatcher, syncDispatcher rest2EthSyncDispatcher) *rest2eth {
	return &rest2eth{
		gw:              gw,
		syncDispatcher:  syncDispatcher,
		asyncDispatcher: asyncDispatcher,
		rpc:             rpc,
		subMgr:          subMgr,
		rr:              rr,
	}
}

func (r *rest2eth) addRoutes(router *httprouter.Router) {
	// Built-in registry managed routes
	router.POST("/contracts/:address/:method", r.restHandler)
	router.GET("/contracts/:address/:method", r.restHandler)
	router.POST("/contracts/:address/:method/:subcommand", r.restHandler)

	router.POST("/abis/:abi", r.restHandler)
	router.POST("/abis/:abi/:address/:method", r.restHandler)
	router.GET("/abis/:abi/:address/:method", r.restHandler)
	router.POST("/abis/:abi/:address/:method/:subcommand", r.restHandler)

	// Remote registry managed address routes, with long and short names
	router.POST("/instances/:instance_lookup/:method", r.restHandler)
	router.GET("/instances/:instance_lookup/:method", r.restHandler)
	router.POST("/instances/:instance_lookup/:method/:subcommand", r.restHandler)

	router.POST("/i/:instance_lookup/:method", r.restHandler)
	router.GET("/i/:instance_lookup/:method", r.restHandler)
	router.POST("/i/:instance_lookup/:method/:subcommand", r.restHandler)

	router.POST("/gateways/:gateway_lookup", r.restHandler)
	router.POST("/gateways/:gateway_lookup/:address/:method", r.restHandler)
	router.GET("/gateways/:gateway_lookup/:address/:method", r.restHandler)
	router.POST("/gateways/:gateway_lookup/:address/:method/:subcommand", r.restHandler)

	router.POST("/g/:gateway_lookup", r.restHandler)
	router.POST("/g/:gateway_lookup/:address/:method", r.restHandler)
	router.GET("/g/:gateway_lookup/:address/:method", r.restHandler)
	router.POST("/g/:gateway_lookup/:address/:method/:subcommand", r.restHandler)
}

type restCmd struct {
	from        string
	addr        string
	value       json.Number
	abiMethod   *abi.Method
	abiEvent    *abi.Event
	isDeploy    bool
	deployMsg   *kldmessages.DeployContract
	body        map[string]interface{}
	msgParams   []interface{}
	blocknumber string
}

func (r *rest2eth) resolveParams(res http.ResponseWriter, req *http.Request, params httprouter.Params) (c restCmd, err error) {

	// Check if we have a valid address in :address (verified later if required)
	addrParam := params.ByName("address")
	c.addr = strings.ToLower(strings.TrimPrefix(addrParam, "0x"))
	validAddress := addrCheck.MatchString(c.addr)

	// There are multiple ways we resolve the path into an ABI
	// 1. we lookup it up remotely in a REST attached contract registry (the newer option)
	//    - /gateways  is for factory interfaces that can talk to any instance
	//    - /instances is for known individual instances
	// 2. we lookup it up locally in a simple filestore managed in ethconnect (the original option)
	//    - /abis      is for factory interfaces installed into ethconnect by uploading the Solidity
	//    - /contracts is for individual instances deployed via ethconnect factory interfaces
	var a *kldbind.ABI
	if strings.HasPrefix(req.URL.Path, "/gateways/") || strings.HasPrefix(req.URL.Path, "/g/") {
		c.deployMsg, err = r.rr.loadFactoryForGateway(params.ByName("gateway_lookup"))
		if err != nil {
			r.restErrReply(res, req, err, 500)
			return
		} else if c.deployMsg == nil {
			err = klderrors.Errorf(klderrors.RESTGatewayGatewayNotFound)
			r.restErrReply(res, req, err, 404)
			return
		}
	} else if strings.HasPrefix(req.URL.Path, "/instances/") || strings.HasPrefix(req.URL.Path, "/i/") {
		var msg *deployContractWithAddress
		msg, err = r.rr.loadFactoryForInstance(params.ByName("instance_lookup"))
		if err != nil {
			r.restErrReply(res, req, err, 500)
			return
		} else if msg == nil {
			err = klderrors.Errorf(klderrors.RESTGatewayInstanceNotFound)
			r.restErrReply(res, req, err, 404)
			return
		}
		c.deployMsg = &msg.DeployContract
		c.addr = msg.Address
		validAddress = true // assume registry only returns valid addresses
	} else {
		// Local logic
		abiID := params.ByName("abi")
		if abiID != "" {
			c.deployMsg, _, err = r.gw.loadDeployMsgByID(abiID)
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
				addrParam = c.addr
			}
			c.deployMsg, _, err = r.gw.loadDeployMsgForInstance(addrParam)
			if err != nil {
				r.restErrReply(res, req, err, 404)
				return c, err
			}
		}
	}
	a = c.deployMsg.ABI

	// See addRoutes for all the various routes we support under the factory/instance.
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
		c.isDeploy = true
	}

	// If we didn't find the method or event, report to the user
	if c.abiMethod == nil && c.abiEvent == nil {
		if methodParamLC == "subscribe" {
			err = klderrors.Errorf(klderrors.RESTGatewayEventNotDeclared, methodParam)
			r.restErrReply(res, req, err, 404)
			return
		}
		err = klderrors.Errorf(klderrors.RESTGatewayMethodNotDeclared, url.QueryEscape(methodParam), c.addr)
		r.restErrReply(res, req, err, 404)
		return
	}

	// If we have an address, it must be valid
	if c.addr != "" && !validAddress {
		log.Errorf("Invalid to addres: '%s'", params.ByName("address"))
		err = klderrors.Errorf(klderrors.RESTGatewayInvalidToAddress)
		r.restErrReply(res, req, err, 404)
		return
	}
	if c.addr != "" {
		c.addr = "0x" + c.addr
	}

	// If we have a from, it needs to be a valid address
	kldFrom := getKLDParam("from", req, false)
	fromNo0xPrefix := strings.ToLower(strings.TrimPrefix(getKLDParam("from", req, false), "0x"))
	if fromNo0xPrefix != "" {
		if addrCheck.MatchString(fromNo0xPrefix) {
			c.from = "0x" + fromNo0xPrefix
		} else if kldtx.IsHDWalletRequest(fromNo0xPrefix) != nil {
			c.from = fromNo0xPrefix
		} else {
			log.Errorf("Invalid from address: '%s'", kldFrom)
			err = klderrors.Errorf(klderrors.RESTGatewayInvalidFromAddress)
			r.restErrReply(res, req, err, 404)
			return
		}
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
			err = klderrors.Errorf(klderrors.RESTGatewayMissingParameter, abiParam.Name, c.abiMethod.Name)
			r.restErrReply(res, req, err, 400)
			return
		}
		c.msgParams = append(c.msgParams, msgParam)
	}

	c.blocknumber = getKLDParam("blocknumber", req, false)

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
	} else if (req.Method == http.MethodPost && !c.abiMethod.Const) && strings.ToLower(getKLDParam("call", req, true)) != "true" {
		if c.from == "" {
			err = klderrors.Errorf(klderrors.RESTGatewayMissingFromAddress)
			r.restErrReply(res, req, err, 400)
		} else if c.isDeploy {
			r.deployContract(res, req, c.from, c.value, c.abiMethod, c.deployMsg, c.msgParams)
		} else {
			r.sendTransaction(res, req, c.from, c.addr, c.value, c.abiMethod, c.msgParams)
		}
	} else {
		r.callContract(res, req, c.from, c.addr, c.value, c.abiMethod, c.msgParams, c.blocknumber)
	}
}

func (r *rest2eth) fromBodyOrForm(req *http.Request, body map[string]interface{}, param string) string {
	val := body[param]
	valType := reflect.TypeOf(val)
	if valType != nil && valType.Kind() == reflect.String && len(val.(string)) > 0 {
		return val.(string)
	}
	return req.FormValue(param)
}

func (r *rest2eth) subscribeEvent(res http.ResponseWriter, req *http.Request, addrStr string, abiEvent *abi.Event, body map[string]interface{}) {

	err := kldauth.AuthEventStreams(req.Context())
	if err != nil {
		log.Errorf("Unauthorized: %s", err)
		r.restErrReply(res, req, klderrors.Errorf(klderrors.Unauthorized), 401)
		return
	}

	if r.subMgr == nil {
		r.restErrReply(res, req, errors.New(errEventSupportMissing), 405)
		return
	}
	streamID := r.fromBodyOrForm(req, body, "stream")
	if streamID == "" {
		r.restErrReply(res, req, klderrors.Errorf(klderrors.RESTGatewaySubscribeMissingStreamParameter), 400)
		return
	}
	fromBlock := r.fromBodyOrForm(req, body, "fromBlock")
	var addr *common.Address
	if addrStr != "" {
		address := common.HexToAddress(addrStr)
		addr = &address
	}
	sub, err := r.subMgr.AddSubscription(req.Context(), addr, abiEvent, streamID, fromBlock)
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

func (r *rest2eth) addPrivateTx(msg *kldmessages.TransactionCommon, req *http.Request, res http.ResponseWriter) error {
	msg.PrivateFrom = r.doubleURLDecode(getKLDParam("privatefrom", req, false))
	msg.PrivateFor = getKLDParamMulti("privatefor", req)
	for idx, val := range msg.PrivateFor {
		msg.PrivateFor[idx] = r.doubleURLDecode(val)
	}
	msg.PrivacyGroupID = r.doubleURLDecode(getKLDParam("privacygroupid", req, false))
	if len(msg.PrivateFor) > 0 && msg.PrivacyGroupID != "" {
		return klderrors.Errorf(klderrors.RESTGatewayMixedPrivateForAndGroupID)
	}
	return nil
}

func (r *rest2eth) deployContract(res http.ResponseWriter, req *http.Request, from string, value json.Number, abiMethod *abi.Method, deployMsg *kldmessages.DeployContract, msgParams []interface{}) {

	deployMsg.Headers.MsgType = kldmessages.MsgTypeDeployContract
	deployMsg.From = from
	deployMsg.Gas = json.Number(getKLDParam("gas", req, false))
	deployMsg.GasPrice = json.Number(getKLDParam("gasprice", req, false))
	deployMsg.Value = value
	deployMsg.Parameters = msgParams
	if err := r.addPrivateTx(&deployMsg.TransactionCommon, req, res); err != nil {
		r.restErrReply(res, req, err, 400)
		return
	}
	deployMsg.RegisterAs = getKLDParam("register", req, false)
	if deployMsg.RegisterAs != "" {
		if err := r.gw.checkNameAvailable(deployMsg.RegisterAs, isRemote(deployMsg.Headers.CommonHeaders)); err != nil {
			r.restErrReply(res, req, err, 409)
			return
		}
	}
	if strings.ToLower(getKLDParam("sync", req, true)) == "true" {
		responder := &rest2EthSyncResponder{
			r:      r,
			res:    res,
			req:    req,
			done:   false,
			waiter: sync.NewCond(&sync.Mutex{}),
		}
		r.syncDispatcher.DispatchDeployContractSync(req.Context(), deployMsg, responder)
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
		if asyncResponse, err := r.asyncDispatcher.DispatchMsgAsync(req.Context(), mapMsg, ack); err != nil {
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
	msg.To = addr
	msg.From = from
	msg.Gas = json.Number(getKLDParam("gas", req, false))
	msg.GasPrice = json.Number(getKLDParam("gasprice", req, false))
	msg.Value = value
	msg.Parameters = msgParams
	if err := r.addPrivateTx(&msg.TransactionCommon, req, res); err != nil {
		r.restErrReply(res, req, err, 400)
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
		r.syncDispatcher.DispatchSendTransactionSync(req.Context(), msg, responder)
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
		if asyncResponse, err := r.asyncDispatcher.DispatchMsgAsync(req.Context(), mapMsg, ack); err != nil {
			r.restErrReply(res, req, err, 500)
		} else {
			r.restAsyncReply(res, req, asyncResponse)
		}
	}
	return
}

func (r *rest2eth) callContract(res http.ResponseWriter, req *http.Request, from, addr string, value json.Number, abiMethod *abi.Method, msgParams []interface{}, blocknumber string) {
	resBody, err := kldeth.CallMethod(req.Context(), r.rpc, nil, from, addr, value, abiMethod, msgParams, blocknumber)
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
