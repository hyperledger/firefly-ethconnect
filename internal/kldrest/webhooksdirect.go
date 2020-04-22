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
	"fmt"
	"sync"
	"time"

	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldtx"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
)

// WebhooksDirectConf defines the YAML structore for a Webhooks direct to RPC bridge
type WebhooksDirectConf struct {
	MaxInFlight int `json:"maxInFlight"`
	kldtx.TxnProcessorConf
	kldeth.RPCConf
}

// webhooksDirect provides the HTTP -> Kafka bridge functionality for ethconnect
type webhooksDirect struct {
	initialized   bool
	receipts      *receiptStore
	conf          *WebhooksDirectConf
	processor     kldtx.TxnProcessor
	inFlightMutex sync.Mutex
	inFlight      map[string]*msgContext
	stopChan      chan error
}

func newWebhooksDirect(conf *WebhooksDirectConf, processor kldtx.TxnProcessor, receipts *receiptStore) *webhooksDirect {
	return &webhooksDirect{
		processor: processor,
		receipts:  receipts,
		conf:      conf,
		inFlight:  make(map[string]*msgContext),
		stopChan:  make(chan error),
	}
}

type msgContext struct {
	ctx          context.Context
	w            *webhooksDirect
	timeReceived time.Time
	key          string
	msgID        string
	msg          map[string]interface{}
	headers      *kldmessages.CommonHeaders
}

func (t *msgContext) Context() context.Context {
	return t.ctx
}

func (t *msgContext) Headers() *kldmessages.CommonHeaders {
	return t.headers
}

func (t *msgContext) Unmarshal(msg interface{}) error {
	msgBytes, err := json.Marshal(t.msg)
	if err != nil {
		return err
	}
	return json.Unmarshal(msgBytes, msg)
}

func (t *msgContext) SendErrorReply(status int, err error) {
	t.SendErrorReplyWithGapFill(status, err, "", false)
}

func (t *msgContext) SendErrorReplyWithGapFill(status int, err error, gapFillTxHash string, gapFillSucceeded bool) {
	t.SendErrorReplyWithTX(status, err, "")
}

func (t *msgContext) SendErrorReplyWithTX(status int, err error, txHash string) {
	log.Warnf("Failed to process message %s: %s", t, err)
	origBytes, _ := json.Marshal(t.msg)
	errMsg := kldmessages.NewErrorReply(err, origBytes)
	errMsg.TXHash = txHash
	t.Reply(errMsg)
}

func (t *msgContext) Reply(replyMessage kldmessages.ReplyWithHeaders) {
	t.w.inFlightMutex.Lock()
	defer t.w.inFlightMutex.Unlock()

	replyHeaders := replyMessage.ReplyHeaders()
	replyHeaders.ID = kldutils.UUIDv4()
	replyHeaders.Context = t.headers.Context
	replyHeaders.ReqID = t.headers.ID
	replyHeaders.Received = t.timeReceived.UTC().Format(time.RFC3339Nano)
	replyTime := time.Now().UTC()
	replyHeaders.Elapsed = replyTime.Sub(t.timeReceived).Seconds()
	msgBytes, _ := json.Marshal(&replyMessage)
	t.w.receipts.processReply(msgBytes)
	delete(t.w.inFlight, t.msgID)
}

func (t *msgContext) String() string {
	return fmt.Sprintf("MsgContext[%s/%s]", t.headers.MsgType, t.msgID)
}

func (w *webhooksDirect) sendWebhookMsg(ctx context.Context, key, msgID string, msg map[string]interface{}, ack bool) (string, int, error) {
	w.inFlightMutex.Lock()

	numInFlight := len(w.inFlight)
	if numInFlight >= w.conf.MaxInFlight {
		w.inFlightMutex.Unlock()
		log.Errorf("Failed to dispatch mesage from '%s': %d/%d already in-flight", key, numInFlight, w.conf.MaxInFlight)
		return "", 429, klderrors.Errorf(klderrors.WebhooksDirectTooManyInflight)
	}

	var headers kldmessages.CommonHeaders
	var headerBytes []byte
	var err error
	headersMap := msg["headers"]
	if headerBytes, err = json.Marshal(&headersMap); err == nil {
		err = json.Unmarshal(headerBytes, &headers)
	}
	if err != nil {
		w.inFlightMutex.Unlock()
		log.Errorf("Unable to unmarshal headers from map payload: %+v: %s", msg, err)
		return "", 400, klderrors.Errorf(klderrors.WebhooksDirectBadHeaders)
	}
	msgContext := &msgContext{
		ctx:          ctx,
		w:            w,
		timeReceived: time.Now().UTC(),
		key:          key,
		msgID:        msgID,
		msg:          msg,
		headers:      &headers,
	}
	w.inFlight[msgID] = msgContext
	w.inFlightMutex.Unlock()

	w.processor.OnMessage(msgContext)
	return "", 200, nil
}

func validateWebhooksDirectConf(conf *WebhooksDirectConf) error {
	if conf.RPC.URL == "" {
		return klderrors.Errorf(klderrors.ConfigWebhooksDirectRPC)
	}
	if conf.MaxTXWaitTime < 10 {
		if conf.MaxTXWaitTime > 0 {
			log.Warnf("Maximum wait time increased from %d to minimum of 10 seconds", conf.MaxTXWaitTime)
		}
		conf.MaxTXWaitTime = 10
	}
	if conf.MaxInFlight <= 0 {
		conf.MaxInFlight = 10
	}
	return nil
}

func (w *webhooksDirect) run() error {
	w.initialized = true
	return <-w.stopChan
}

func (w *webhooksDirect) isInitialized() bool {
	return w.initialized
}
