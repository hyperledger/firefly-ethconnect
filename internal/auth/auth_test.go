// Copyright 2018, 2021 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package auth

import (
	"context"
	"testing"

	"github.com/hyperledger/firefly-ethconnect/internal/auth/authtest"
	"github.com/stretchr/testify/assert"
)

func TestSystemContext(t *testing.T) {
	assert := assert.New(t)

	assert.True(IsSystemContext(NewSystemAuthContext()))
	assert.False(IsSystemContext(context.Background()))
	assert.False(IsSystemContext(context.WithValue(context.Background(), ContextKeySystemAuth, "false")))
	assert.False(IsSystemContext(context.WithValue(context.Background(), ContextKeySystemAuth, false)))

}

func TestAccessToken(t *testing.T) {
	assert := assert.New(t)

	ctx, err := WithAuthContext(context.Background(), "testat")
	assert.NoError(err)
	assert.Equal("", GetAccessToken(ctx))
	assert.Equal(nil, GetAuthContext(ctx))

	RegisterSecurityModule(&authtest.TestSecurityModule{})

	ctx, err = WithAuthContext(context.Background(), "testat")
	assert.NoError(err)
	assert.Equal("verified", GetAuthContext(ctx))
	assert.Equal("testat", GetAccessToken(ctx))

	assert.Equal(nil, GetAuthContext(context.Background()))
	assert.Equal("", GetAccessToken(context.Background()))

	_, err = WithAuthContext(context.Background(), "badone")
	assert.Regexp("badness", err)

	RegisterSecurityModule(nil)
}

func TestAuthRPC(t *testing.T) {
	assert := assert.New(t)

	assert.NoError(AuthRPC(context.Background(), "anything"))

	RegisterSecurityModule(&authtest.TestSecurityModule{})

	assert.Regexp("No auth context", AuthRPC(context.Background(), "anything"))

	assert.NoError(AuthRPC(NewSystemAuthContext(), "anything"))

	ctx, _ := WithAuthContext(context.Background(), "testat")
	assert.NoError(AuthRPC(ctx, "testrpc"))
	assert.Regexp("badness", AuthRPC(ctx, "anything"))

	RegisterSecurityModule(nil)

}

func TestAuthRPCSubscribe(t *testing.T) {
	assert := assert.New(t)

	assert.NoError(AuthRPCSubscribe(context.Background(), "anything", nil))

	RegisterSecurityModule(&authtest.TestSecurityModule{})

	assert.Regexp("No auth context", AuthRPCSubscribe(context.Background(), "anything", nil))

	assert.NoError(AuthRPCSubscribe(NewSystemAuthContext(), "anything", nil))

	ctx, _ := WithAuthContext(context.Background(), "testat")
	assert.NoError(AuthRPCSubscribe(ctx, "testns", nil))
	assert.Regexp("badness", AuthRPCSubscribe(ctx, "anything", nil))

	RegisterSecurityModule(nil)

}

func TestAuthEventStreams(t *testing.T) {
	assert := assert.New(t)

	assert.NoError(AuthEventStreams(context.Background()))

	RegisterSecurityModule(&authtest.TestSecurityModule{})

	assert.Regexp("No auth context", AuthEventStreams(context.Background()))

	assert.NoError(AuthEventStreams(NewSystemAuthContext()))

	ctx, _ := WithAuthContext(context.Background(), "testat")
	assert.NoError(AuthEventStreams(ctx))

	RegisterSecurityModule(nil)

}

func TestAuthListAsyncReplies(t *testing.T) {
	assert := assert.New(t)

	assert.NoError(AuthListAsyncReplies(context.Background()))

	RegisterSecurityModule(&authtest.TestSecurityModule{})

	assert.Regexp("No auth context", AuthListAsyncReplies(context.Background()))

	assert.NoError(AuthListAsyncReplies(NewSystemAuthContext()))

	ctx, _ := WithAuthContext(context.Background(), "testat")
	assert.NoError(AuthListAsyncReplies(ctx))

	RegisterSecurityModule(nil)

}

func TestAuthReadAsyncReplyByUUID(t *testing.T) {
	assert := assert.New(t)

	assert.NoError(AuthReadAsyncReplyByUUID(context.Background()))

	RegisterSecurityModule(&authtest.TestSecurityModule{})

	assert.Regexp("No auth context", AuthReadAsyncReplyByUUID(context.Background()))

	assert.NoError(AuthReadAsyncReplyByUUID(NewSystemAuthContext()))

	ctx, _ := WithAuthContext(context.Background(), "testat")
	assert.NoError(AuthReadAsyncReplyByUUID(ctx))

	RegisterSecurityModule(nil)

}
