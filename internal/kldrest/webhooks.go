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

package kldrest

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldcontracts"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
)

type webhooksHandler interface {
	sendWebhookMsg(ctx context.Context, key, msgID string, msg map[string]interface{}, ack bool) (msgAck string, statusCode int, err error)
	run() error
	isInitialized() bool
}

// webhooks provides the async HTTP to eth TX bridge
type webhooks struct {
	smartContractGW kldcontracts.SmartContractGateway
	handler         webhooksHandler
}

func newWebhooks(handler webhooksHandler, smartContractGW kldcontracts.SmartContractGateway) *webhooks {
	return &webhooks{
		handler:         handler,
		smartContractGW: smartContractGW,
	}
}

type hookErrMsg struct {
	Sent    bool   `json:"sent"`
	Message string `json:"error"`
}

func (w *webhooks) hookErrReply(res http.ResponseWriter, req *http.Request, err error, status int) {
	log.Errorf("<-- %s %s [%d]: %s", req.Method, req.URL, status, err)
	reply, _ := json.Marshal(&hookErrMsg{Message: err.Error()})
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	res.Write(reply)
	return
}

func (w *webhooks) msgSentReply(res http.ResponseWriter, req *http.Request, replyMsg *kldmessages.AsyncSentMsg) {
	reply, _ := json.Marshal(replyMsg)
	status := 200
	log.Infof("<-- %s %s [%d]: Webhook RequestID=%s", req.Method, req.URL, status, replyMsg.Request)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	res.Write(reply)
	return
}

func (w *webhooks) addRoutes(router *httprouter.Router) {
	router.POST("/", w.webhookHandlerNoAck) // Default on base URL
	router.POST("/hook", w.webhookHandlerWithAck)
	router.POST("/fasthook", w.webhookHandlerNoAck)
}

func (w *webhooks) webhookHandlerWithAck(res http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	w.webhookHandler(res, req, true)
}

func (w *webhooks) webhookHandlerNoAck(res http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	w.webhookHandler(res, req, false)
}

func (w *webhooks) webhookHandler(res http.ResponseWriter, req *http.Request, ack bool) {
	log.Infof("--> %s %s", req.Method, req.URL)

	msg, err := kldutils.YAMLorJSONPayload(req)
	if err != nil {
		w.hookErrReply(res, req, err, 400)
		return
	}

	reply, statusCode, err := w.processMsg(req.Context(), msg, ack)
	if err != nil {
		w.hookErrReply(res, req, err, statusCode)
		return
	}
	w.msgSentReply(res, req, reply)
}

func (w *webhooks) processMsg(ctx context.Context, msg map[string]interface{}, ack bool) (*kldmessages.AsyncSentMsg, int, error) {
	// Check we understand the type, and can get the key.
	// The rest of the validation is performed by the bridge listening to Kafka
	headers, exists := msg["headers"]
	if !exists || reflect.TypeOf(headers).Kind() != reflect.Map {
		return nil, 400, klderrors.Errorf(klderrors.WebhooksInvalidMsgHeaders)
	}
	msgType, exists := headers.(map[string]interface{})["type"]
	if !exists || reflect.TypeOf(msgType).Kind() != reflect.String {
		return nil, 400, klderrors.Errorf(klderrors.WebhooksInvalidMsgTypeMissing)
	}
	var key string
	switch msgType {
	case kldmessages.MsgTypeDeployContract, kldmessages.MsgTypeSendTransaction:
		from, exists := msg["from"]
		if !exists || reflect.TypeOf(from).Kind() != reflect.String {
			return nil, 400, klderrors.Errorf(klderrors.WebhooksInvalidMsgFromMissing)
		}
		key = from.(string)
		break
	default:
		return nil, 400, klderrors.Errorf(klderrors.WebhooksInvalidMsgType, msgType)
	}

	// We always generate the ID. It cannot be set by the user
	msgID := kldutils.UUIDv4()
	headers.(map[string]interface{})["id"] = msgID

	if w.smartContractGW != nil && msgType == kldmessages.MsgTypeDeployContract {
		var err error
		if msg, err = w.contractGWHandler(msg); err != nil {
			return nil, 500, err
		}
	}

	// Pass to the handler
	log.Infof("Webhook accepted message. MsgID: %s Type: %s", msgID, msgType)
	msgAck, status, err := w.handler.sendWebhookMsg(ctx, key, msgID, msg, ack)
	if err != nil {
		return nil, status, err
	}
	return &kldmessages.AsyncSentMsg{
		Sent:    true,
		Request: msgID,
		Msg:     msgAck,
	}, 200, nil
}

func (w *webhooks) contractGWHandler(msg map[string]interface{}) (map[string]interface{}, error) {
	// We have to fully parse, then re-serialize, the message in the case of a contract deployment
	// where we are performing OpenAPI gateway processing
	msgBytes, _ := json.Marshal(&msg)
	var deployMsg kldmessages.DeployContract
	if err := json.Unmarshal(msgBytes, &deployMsg); err != nil {
		return nil, err
	}

	// Call the GW handler
	if err := w.smartContractGW.PreDeploy(&deployMsg); err != nil {
		return nil, err
	}

	// Now send the message back to a generic map
	msgBytes, _ = json.Marshal(&deployMsg)
	var newMsg map[string]interface{}
	json.Unmarshal(msgBytes, &newMsg)
	return newMsg, nil
}

func (w *webhooks) run() error {
	return w.handler.run()
}

func (w *webhooks) isInitialized() bool {
	return w.handler.isInitialized()
}
