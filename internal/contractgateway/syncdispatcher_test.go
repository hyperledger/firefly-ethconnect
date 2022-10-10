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

package contractgateway

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperledger/firefly-ethconnect/internal/eth"
	"github.com/hyperledger/firefly-ethconnect/internal/messages"
	"github.com/hyperledger/firefly-ethconnect/internal/receipts"
	"github.com/hyperledger/firefly-ethconnect/internal/tx"
	"github.com/stretchr/testify/assert"
)

type mockProcessor struct {
	t            *testing.T
	headers      *messages.CommonHeaders
	err          error
	reply        messages.ReplyWithHeaders
	unmarshalErr error
	badUnmarshal bool
	resolvedFrom string
}

func (p *mockProcessor) ResolveAddress(from string) (resolvedFrom string, err error) {
	return p.resolvedFrom, p.err
}

func (p *mockProcessor) OnMessage(c tx.TxnContext) {
	p.headers = c.Headers()
	ctx := c.(*syncTxInflight)
	if p.badUnmarshal {
		// Send something unexpected
		p.unmarshalErr = c.Unmarshal(&messages.ErrorReply{})
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
func (p *mockProcessor) Init(eth.RPCClient) {}
func (p *mockProcessor) SetReceiptStoreForIdempotencyCheck(receiptStore receipts.ReceiptStorePersistence) {
}

type mockReplyProcessor struct {
	err     error
	receipt messages.ReplyWithHeaders
}

func (p *mockReplyProcessor) ReplyWithError(err error) {
	p.err = err
}

func (p *mockReplyProcessor) ReplyWithReceipt(receipt messages.ReplyWithHeaders) {
	p.receipt = receipt
}

func (p *mockReplyProcessor) ReplyWithReceiptAndError(receipt messages.ReplyWithHeaders, err error) {
	p.receipt = receipt
}

func TestDispatchSendTransactionSync(t *testing.T) {
	assert := assert.New(t)

	processor := &mockProcessor{
		t:     t,
		reply: &messages.TransactionReceipt{},
	}
	d := newSyncDispatcher(processor)
	sendTx := &messages.SendTransaction{}
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
		reply: &messages.TransactionReceipt{},
	}
	d := newSyncDispatcher(processor)
	deployTx := &messages.DeployContract{}
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
		reply:        &messages.TransactionReceipt{},
		badUnmarshal: true,
	}
	d := newSyncDispatcher(processor)
	sendTx := &messages.SendTransaction{}
	sendTx.Headers.ID = "request1"
	r := &mockReplyProcessor{}
	d.DispatchSendTransactionSync(context.Background(), sendTx, r)

	assert.Regexp("Unexpected condition \\(message types do not match when processing\\)", processor.unmarshalErr)
}

func TestDispatchSendTransactionError(t *testing.T) {
	assert := assert.New(t)

	processor := &mockProcessor{
		t:     t,
		reply: &messages.TransactionReceipt{},
		err:   fmt.Errorf("pop"),
	}
	d := newSyncDispatcher(processor)
	sendTx := &messages.SendTransaction{}
	sendTx.Headers.ID = "request1"
	r := &mockReplyProcessor{}
	d.DispatchSendTransactionSync(context.Background(), sendTx, r)

	assert.Regexp("TX hash1: pop", r.err)
}
