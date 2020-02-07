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
	"testing"

	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldtx"
	"github.com/stretchr/testify/assert"
)

type mockProcessor struct {
	t            *testing.T
	headers      *kldmessages.CommonHeaders
	err          error
	reply        kldmessages.ReplyWithHeaders
	unmarshalErr error
	badUnmarshal bool
}

func (p *mockProcessor) OnMessage(c kldtx.TxnContext) {
	p.headers = c.Headers()
	ctx := c.(*syncTxInflight)
	if p.badUnmarshal {
		// Send something unexpected
		p.unmarshalErr = c.Unmarshal(&kldmessages.ErrorReply{})
	} else if ctx.sendMsg != nil {
		p.unmarshalErr = c.Unmarshal(ctx.sendMsg)
	} else {
		p.unmarshalErr = c.Unmarshal(ctx.deployMsg)
	}
	p.t.Logf("string value: %s", c)
	if p.err != nil {
		c.SendErrorReplyWithTX(0, p.err, "hash1")
	} else {
		c.Reply(p.reply)
	}
}
func (p *mockProcessor) Init(kldeth.RPCClient) {}

type mockReplyProcessor struct {
	err     error
	receipt kldmessages.ReplyWithHeaders
}

func (p *mockReplyProcessor) ReplyWithError(err error) {
	p.err = err
}

func (p *mockReplyProcessor) ReplyWithReceipt(receipt kldmessages.ReplyWithHeaders) {
	p.receipt = receipt
}

func (p *mockReplyProcessor) ReplyWithReceiptAndError(receipt kldmessages.ReplyWithHeaders, err error) {
	p.receipt = receipt
}

func TestDispatchSendTransactionSync(t *testing.T) {
	assert := assert.New(t)

	processor := &mockProcessor{
		t:     t,
		reply: &kldmessages.TransactionReceipt{},
	}
	d := newSyncDispatcher(processor)
	sendTx := &kldmessages.SendTransaction{}
	sendTx.Headers.ID = "request1"
	r := &mockReplyProcessor{}
	d.DispatchSendTransactionSync(context.Background(), sendTx, r)

	assert.NoError(processor.unmarshalErr)
	assert.NotNil(r.receipt)
}

func TestDispatchDeployContractSync(t *testing.T) {
	assert := assert.New(t)

	processor := &mockProcessor{
		t:     t,
		reply: &kldmessages.TransactionReceipt{},
	}
	d := newSyncDispatcher(processor)
	deployTx := &kldmessages.DeployContract{}
	deployTx.Headers.ID = "request1"
	r := &mockReplyProcessor{}
	d.DispatchDeployContractSync(context.Background(), deployTx, r)

	assert.NoError(processor.unmarshalErr)
	assert.NotNil(r.receipt)
}

func TestDispatchSendTransactionBadUnmarshal(t *testing.T) {
	assert := assert.New(t)

	processor := &mockProcessor{
		t:            t,
		reply:        &kldmessages.TransactionReceipt{},
		badUnmarshal: true,
	}
	d := newSyncDispatcher(processor)
	sendTx := &kldmessages.SendTransaction{}
	sendTx.Headers.ID = "request1"
	r := &mockReplyProcessor{}
	d.DispatchSendTransactionSync(context.Background(), sendTx, r)

	assert.EqualError(processor.unmarshalErr, "Unexpected condition (message types do not match when processing)")
}

func TestDispatchSendTransactionError(t *testing.T) {
	assert := assert.New(t)

	processor := &mockProcessor{
		t:     t,
		reply: &kldmessages.TransactionReceipt{},
		err:   fmt.Errorf("pop"),
	}
	d := newSyncDispatcher(processor)
	sendTx := &kldmessages.SendTransaction{}
	sendTx.Headers.ID = "request1"
	r := &mockReplyProcessor{}
	d.DispatchSendTransactionSync(context.Background(), sendTx, r)

	assert.EqualError(r.err, "TX hash1: pop")
}
