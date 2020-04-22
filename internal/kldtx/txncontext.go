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

package kldtx

import (
	"context"

	"github.com/kaleido-io/ethconnect/internal/kldmessages"
)

// TxnContext is passed for each message that arrives at the bridge
type TxnContext interface {
	// Return the Go context
	Context() context.Context
	// Get the headers of the message
	Headers() *kldmessages.CommonHeaders
	// Unmarshal the supplied message into a give type
	Unmarshal(msg interface{}) error
	// Send an error reply
	SendErrorReply(status int, err error)
	// Send an error reply
	SendErrorReplyWithTX(status int, err error, txHash string)
	// Send an error reply
	SendErrorReplyWithGapFill(status int, err error, gapFillTxHash string, gapFillSucceeded bool)
	// Send a reply that can be marshaled into bytes.
	// Sets all the common headers on behalf of the caller, based on the request context
	Reply(replyMsg kldmessages.ReplyWithHeaders)
	// Get a string summary
	String() string
}
