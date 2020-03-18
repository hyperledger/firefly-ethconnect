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

package kldrest

import (
	"encoding/json"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldauth"
	"github.com/kaleido-io/ethconnect/internal/kldcontracts"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
)

const (
	defaultReceiptLimit = 10
)

var uuidCharsVerifier, _ = regexp.Compile("^[0-9a-zA-Z-]+$")

// ReceiptStorePersistence interface implemented by persistence layers
type ReceiptStorePersistence interface {
	GetReceipts(skip, limit int, ids []string, sinceEpochMS int64, from, to string) (*[]map[string]interface{}, error)
	GetReceipt(requestID string) (*map[string]interface{}, error)
	AddReceipt(receipt *map[string]interface{}) error
}

type receiptStore struct {
	conf            *ReceiptStoreConf
	persistence     ReceiptStorePersistence
	smartContractGW kldcontracts.SmartContractGateway
}

func newReceiptStore(conf *ReceiptStoreConf, persistence ReceiptStorePersistence, smartContractGW kldcontracts.SmartContractGateway) *receiptStore {
	return &receiptStore{
		conf:            conf,
		persistence:     persistence,
		smartContractGW: smartContractGW,
	}
}

func (r *receiptStore) addRoutes(router *httprouter.Router) {
	router.GET("/replies", r.getReplies)
	router.GET("/replies/:id", r.getReply)
	router.GET("/reply/:id", r.getReply)
}

func (r *receiptStore) processReply(msgBytes []byte) {

	// Parse the reply as JSON
	var parsedMsg map[string]interface{}
	if err := json.Unmarshal(msgBytes, &parsedMsg); err != nil {
		log.Errorf("Unable to unmarshal reply message '%s' as JSON: %s", string(msgBytes), err)
		return
	}

	// Extract the headers
	var headers map[string]interface{}
	if iHeaders, exists := parsedMsg["headers"]; exists && reflect.TypeOf(headers).Kind() == reflect.Map {
		headers = iHeaders.(map[string]interface{})
	} else {
		log.Errorf("Failed to extract request headers from '%+v'", parsedMsg)
		return
	}

	// The one field we require is the original ID (as it's the key in MongoDB)
	requestID := kldutils.GetMapString(headers, "requestId")
	if requestID == "" {
		log.Errorf("Failed to extract headers.requestId from '%+v'", parsedMsg)
		return
	}
	reqOffset := kldutils.GetMapString(headers, "reqOffset")
	msgType := kldutils.GetMapString(headers, "type")
	contractAddr := kldutils.GetMapString(parsedMsg, "contractAddress")
	result := ""
	if msgType == kldmessages.MsgTypeError {
		result = kldutils.GetMapString(parsedMsg, "errorMessage")
	} else {
		result = kldutils.GetMapString(parsedMsg, "transactionHash")
	}
	log.Infof("Received reply message. requestId='%s' reqOffset='%s' type='%s': %s", requestID, reqOffset, msgType, result)

	if r.smartContractGW != nil && msgType == kldmessages.MsgTypeTransactionSuccess && contractAddr != "" {
		var receipt kldmessages.TransactionReceipt
		if err := json.Unmarshal(msgBytes, &receipt); err == nil {
			if err = r.smartContractGW.PostDeploy(&receipt); err != nil {
				log.Errorf("Failed to process receipt in smart contract gateway: %s", err)
			}
		} else {
			log.Errorf("Failed to parse message as transaction receipt: %s", err)
		}
	}

	parsedMsg["receivedAt"] = time.Now().UnixNano() / int64(time.Millisecond)
	parsedMsg["_id"] = requestID

	// Insert the receipt into MongoDB - captures errors
	if requestID != "" && r.persistence != nil {
		if err := r.persistence.AddReceipt(&parsedMsg); err != nil {
			log.Errorf("Failed to insert '%s' into receipt store: %+v", parsedMsg, err)
		} else {
			log.Infof("Inserted receipt %s into receipt store", parsedMsg["_id"])
		}
	}
}

func (r *receiptStore) marshalAndReply(res http.ResponseWriter, req *http.Request, result interface{}) {
	// Serialize and return
	resBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Errorf("Error serializing receipts: %s", err)
		sendRESTError(res, req, klderrors.Errorf(klderrors.ReceiptStoreSerializeResponse), 500)
		return
	}
	status := 200
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	res.Write(resBytes)
}

// getReplies handles a HTTP request for recent replies
func (r *receiptStore) getReplies(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	err := kldauth.AuthListAsyncReplies(req.Context())
	if err != nil {
		sendRESTError(res, req, klderrors.Errorf(klderrors.Unauthorized), 401)
		return
	}

	res.Header().Set("Content-Type", "application/json")
	if r.persistence == nil {
		sendRESTError(res, req, klderrors.Errorf(klderrors.ReceiptStoreDisabled), 405)
		return
	}

	// Default limit - which is set to zero (infinite) if we have specific IDs being request
	limit := defaultReceiptLimit
	req.ParseForm()
	ids, ok := req.Form["id"]
	if ok {
		limit = 0 // can be explicitly set below, but no imposed limit when we have a list of IDs
		for idx, id := range ids {
			if !uuidCharsVerifier.MatchString(id) {
				log.Errorf("Invalid id '%s' %d", id, idx)
				sendRESTError(res, req, klderrors.Errorf(klderrors.ReceiptStoreInvalidRequestID), 400)
				return
			}
		}
	}

	// Extract limit
	limitStr := req.FormValue("limit")
	if limitStr != "" {
		if customLimit, err := strconv.ParseInt(limitStr, 10, 32); err == nil {
			if int(customLimit) > r.conf.QueryLimit {
				log.Errorf("Invalid limit value: %s", err)
				sendRESTError(res, req, klderrors.Errorf(klderrors.ReceiptStoreInvalidRequestMaxLimit, r.conf.QueryLimit), 400)
				return
			} else if customLimit > 0 {
				limit = int(customLimit)
			}
		} else {
			log.Errorf("Invalid limit value: %s", err)
			sendRESTError(res, req, klderrors.Errorf(klderrors.ReceiptStoreInvalidRequestBadLimit), 400)
			return
		}
	}

	// Extract skip
	var skip int
	skipStr := req.FormValue("skip")
	if skipStr != "" {
		if skipI64, err := strconv.ParseInt(skipStr, 10, 32); err == nil && skipI64 > 0 {
			skip = int(skipI64)
		} else {
			log.Errorf("Invalid skip value: %s", err)
			sendRESTError(res, req, klderrors.Errorf(klderrors.ReceiptStoreInvalidRequestBadSkip), 400)
			return
		}
	}

	// Verify since - if specified
	var sinceEpochMS int64
	since := req.FormValue("since")
	if since != "" {
		if isoTime, err := time.Parse(time.RFC3339Nano, since); err == nil {
			sinceEpochMS = isoTime.UnixNano() / int64(time.Millisecond)
		} else {
			if sinceEpochMS, err = strconv.ParseInt(since, 10, 64); err != nil {
				log.Errorf("since '%s' cannot be parsed as RFC3339 or millisecond timestamp: %s", since, err)
				sendRESTError(res, req, klderrors.Errorf(klderrors.ReceiptStoreInvalidRequestBadSince), 400)
			}
		}
	}

	from := req.FormValue("from")
	to := req.FormValue("to")

	// Call the persistence tier - which must return an empty array when no results (not an error)
	results, err := r.persistence.GetReceipts(skip, limit, ids, sinceEpochMS, from, to)
	if err != nil {
		log.Errorf("Error querying replies: %s", err)
		sendRESTError(res, req, klderrors.Errorf(klderrors.ReceiptStoreFailedQuery, err), 500)
		return
	}
	log.Debugf("Replies query: skip=%d limit=%d replies=%d", skip, limit, len(*results))
	r.marshalAndReply(res, req, results)

}

// getReply handles a HTTP request for an individual reply
func (r *receiptStore) getReply(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	err := kldauth.AuthReadAsyncReplyByUUID(req.Context())
	if err != nil {
		sendRESTError(res, req, klderrors.Errorf(klderrors.Unauthorized), 401)
		return
	}

	requestID := params.ByName("id")
	// Call the persistence tier - which must return an empty array when no results (not an error)
	result, err := r.persistence.GetReceipt(requestID)
	if err != nil {
		log.Errorf("Error querying reply: %s", err)
		sendRESTError(res, req, klderrors.Errorf(klderrors.ReceiptStoreFailedQuerySingle, err), 500)
		return
	} else if result == nil {
		sendRESTError(res, req, klderrors.Errorf(klderrors.ReceiptStoreFailedNotFound), 404)
		log.Infof("Reply not found")
		return
	}
	log.Infof("Reply found")
	r.marshalAndReply(res, req, result)
}
