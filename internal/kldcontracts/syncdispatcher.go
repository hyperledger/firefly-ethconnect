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
	"fmt"
	"reflect"
	"time"

	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldtx"
	"github.com/kaleido-io/ethconnect/internal/kldutils"

	log "github.com/sirupsen/logrus"
)

type syncDispatcher struct {
	processor kldtx.TxnProcessor
}

func newSyncDispatcher(processor kldtx.TxnProcessor) rest2EthSyncDispatcher {
	return &syncDispatcher{
		processor: processor,
	}
}

type syncTxInflight struct {
	ctx            context.Context
	d              *syncDispatcher
	replyProcessor rest2EthReplyProcessor
	timeReceived   time.Time
	sendMsg        *kldmessages.SendTransaction
	deployMsg      *kldmessages.DeployContract
}

func (t *syncTxInflight) Context() context.Context {
	return t.ctx
}

func (t *syncTxInflight) Headers() *kldmessages.CommonHeaders {
	if t.deployMsg != nil {
		return &t.deployMsg.Headers.CommonHeaders
	}
	return &t.sendMsg.Headers.CommonHeaders
}

func (t *syncTxInflight) Unmarshal(msg interface{}) error {
	var retMsg interface{}
	if t.deployMsg != nil {
		retMsg = t.deployMsg
	} else {
		retMsg = t.sendMsg
	}
	if reflect.TypeOf(msg) != reflect.TypeOf(retMsg) {
		log.Errorf("Type mismatch: %s != %s", reflect.TypeOf(msg), reflect.TypeOf(retMsg))
		return klderrors.Errorf(klderrors.RESTGatewaySyncMsgTypeMismatch)
	}
	reflect.ValueOf(msg).Elem().Set(reflect.ValueOf(retMsg).Elem())
	return nil
}

func (t *syncTxInflight) SendErrorReply(status int, err error) {
	t.SendErrorReplyWithGapFill(status, err, "", false)
}

func (t *syncTxInflight) SendErrorReplyWithGapFill(status int, err error, gapFillTxHash string, gapFillSucceeded bool) {
	t.replyProcessor.ReplyWithError(err) // We don't add the gapfill info in sync
}

func (t *syncTxInflight) SendErrorReplyWithTX(status int, err error, txHash string) {
	t.SendErrorReply(status, klderrors.Errorf(klderrors.RESTGatewaySyncWrapErrorWithTXDetail, txHash, err))
}

func (t *syncTxInflight) Reply(replyMessage kldmessages.ReplyWithHeaders) {
	headers := t.Headers()
	replyHeaders := replyMessage.ReplyHeaders()
	replyHeaders.ID = kldutils.UUIDv4()
	replyHeaders.Context = headers.Context
	replyHeaders.ReqID = headers.ID
	replyHeaders.Received = t.timeReceived.UTC().Format(time.RFC3339Nano)
	replyTime := time.Now().UTC()
	replyHeaders.Elapsed = replyTime.Sub(t.timeReceived).Seconds()
	t.replyProcessor.ReplyWithReceipt(replyMessage)
}

func (t *syncTxInflight) String() string {
	headers := t.Headers()
	return fmt.Sprintf("MsgContext[%s/%s]", headers.MsgType, headers.ID)
}

func (d *syncDispatcher) DispatchSendTransactionSync(ctx context.Context, msg *kldmessages.SendTransaction, replyProcessor rest2EthReplyProcessor) {
	syncCtx := &syncTxInflight{
		replyProcessor: replyProcessor,
		timeReceived:   time.Now().UTC(),
		sendMsg:        msg,
		ctx:            ctx,
	}
	d.processor.OnMessage(syncCtx)
}

func (d *syncDispatcher) DispatchDeployContractSync(ctx context.Context, msg *kldmessages.DeployContract, replyProcessor rest2EthReplyProcessor) {
	syncCtx := &syncTxInflight{
		replyProcessor: replyProcessor,
		timeReceived:   time.Now().UTC(),
		deployMsg:      msg,
		ctx:            ctx,
	}
	d.processor.OnMessage(syncCtx)
}
