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

package kldkafka

import (
	"fmt"

	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
)

// MsgProcessor interface is called for each message, as is responsible
// for tracking all in-flight messages
type MsgProcessor interface {
	OnMessage(MsgContext)
	SetRPC(kldeth.RPCClient)
}

type msgProcessor struct {
	rpc kldeth.RPCClient
}

func (p *msgProcessor) SetRPC(rpc kldeth.RPCClient) {
	p.rpc = rpc
}

// OnMessage checks the type and dispatches to the correct logic
// ** From this point on the processor MUST ensure Reply is called
//    on msgContext eventually in all scenarios.
//    It cannot return an error synchronously from this function **
func (p *msgProcessor) OnMessage(msgContext MsgContext) {

	var unmarshalErr error
	headers := msgContext.Headers()
	switch headers.MsgType {
	case kldmessages.MsgTypeDeployContract:
		var deployContractMsg kldmessages.DeployContract
		if unmarshalErr = msgContext.Unmarshal(&deployContractMsg); unmarshalErr != nil {
			break
		}
		p.OnDeployContractMessage(msgContext, &deployContractMsg)
		break
	case kldmessages.MsgTypeSendTransaction:
		var sendTransactionMsg kldmessages.SendTransaction
		if unmarshalErr = msgContext.Unmarshal(&sendTransactionMsg); unmarshalErr != nil {
			break
		}
		p.OnSendTransactionMessage(msgContext, &sendTransactionMsg)
		break
	default:
		unmarshalErr = fmt.Errorf("Unknown message type '%s'", headers.MsgType)
	}
	// We must always send a reply
	if unmarshalErr != nil {
		msgContext.SendErrorReply(400, unmarshalErr)
	}

}

func (p *msgProcessor) OnDeployContractMessage(msgContext MsgContext, msg *kldmessages.DeployContract) {
	tx, err := kldeth.NewContractDeployTxn(msg)
	if err != nil {
		msgContext.SendErrorReply(400, err)
		return
	}

	if err = tx.Send(p.rpc); err != nil {
		msgContext.SendErrorReply(400, err)
		return
	}
}

func (p *msgProcessor) OnSendTransactionMessage(msgContext MsgContext, msg *kldmessages.SendTransaction) {
	tx, err := kldeth.NewSendTxn(msg)
	if err != nil {
		msgContext.SendErrorReply(400, err)
		return
	}

	if err = tx.Send(p.rpc); err != nil {
		msgContext.SendErrorReply(400, err)
		return
	}
}
