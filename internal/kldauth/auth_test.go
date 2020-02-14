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

package kldauth

import (
	"context"
	"testing"

	"github.com/kaleido-io/ethconnect/internal/kldauth/kldauthtest"
	"github.com/stretchr/testify/assert"
)

func TestSystemContext(t *testing.T) {
	assert := assert.New(t)

	assert.True(IsSystemContext(NewSystemAuthContext()))
	assert.False(IsSystemContext(context.Background()))
	assert.False(IsSystemContext(context.WithValue(context.Background(), kldContextKeySystemAuth, "false")))
	assert.False(IsSystemContext(context.WithValue(context.Background(), kldContextKeySystemAuth, false)))

}

func TestAccessToken(t *testing.T) {
	assert := assert.New(t)

	ctx, err := WithAuthContext(context.Background(), "testat")
	assert.NoError(err)
	assert.Equal("", GetAccessToken(ctx))
	assert.Equal(nil, GetAuthContext(ctx))

	RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	ctx, err = WithAuthContext(context.Background(), "testat")
	assert.NoError(err)
	assert.Equal("verified", GetAuthContext(ctx))
	assert.Equal("testat", GetAccessToken(ctx))

	assert.Equal(nil, GetAuthContext(context.Background()))
	assert.Equal("", GetAccessToken(context.Background()))

	ctx, err = WithAuthContext(context.Background(), "badone")
	assert.EqualError(err, "badness")

	RegisterSecurityModule(nil)
}

func TestAuthRPC(t *testing.T) {
	assert := assert.New(t)

	assert.NoError(AuthRPC(context.Background(), "anything"))

	RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	assert.EqualError(AuthRPC(context.Background(), "anything"), "No auth context")

	assert.NoError(AuthRPC(NewSystemAuthContext(), "anything"))

	ctx, _ := WithAuthContext(context.Background(), "testat")
	assert.NoError(AuthRPC(ctx, "testrpc"))
	assert.EqualError(AuthRPC(ctx, "anything"), "badness")

	RegisterSecurityModule(nil)

}

func TestAuthRPCSubscribe(t *testing.T) {
	assert := assert.New(t)

	assert.NoError(AuthRPCSubscribe(context.Background(), "anything", nil))

	RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	assert.EqualError(AuthRPCSubscribe(context.Background(), "anything", nil), "No auth context")

	assert.NoError(AuthRPCSubscribe(NewSystemAuthContext(), "anything", nil))

	ctx, _ := WithAuthContext(context.Background(), "testat")
	assert.NoError(AuthRPCSubscribe(ctx, "testns", nil))
	assert.EqualError(AuthRPCSubscribe(ctx, "anything", nil), "badness")

	RegisterSecurityModule(nil)

}

func TestAuthEventStreams(t *testing.T) {
	assert := assert.New(t)

	assert.NoError(AuthEventStreams(context.Background()))

	RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	assert.EqualError(AuthEventStreams(context.Background()), "No auth context")

	assert.NoError(AuthEventStreams(NewSystemAuthContext()))

	ctx, _ := WithAuthContext(context.Background(), "testat")
	assert.NoError(AuthEventStreams(ctx))

	RegisterSecurityModule(nil)

}

func TestAuthListAsyncReplies(t *testing.T) {
	assert := assert.New(t)

	assert.NoError(AuthListAsyncReplies(context.Background()))

	RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	assert.EqualError(AuthListAsyncReplies(context.Background()), "No auth context")

	assert.NoError(AuthListAsyncReplies(NewSystemAuthContext()))

	ctx, _ := WithAuthContext(context.Background(), "testat")
	assert.NoError(AuthListAsyncReplies(ctx))

	RegisterSecurityModule(nil)

}

func TestAuthReadAsyncReplyByUUID(t *testing.T) {
	assert := assert.New(t)

	assert.NoError(AuthReadAsyncReplyByUUID(context.Background()))

	RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	assert.EqualError(AuthReadAsyncReplyByUUID(context.Background()), "No auth context")

	assert.NoError(AuthReadAsyncReplyByUUID(NewSystemAuthContext()))

	ctx, _ := WithAuthContext(context.Background(), "testat")
	assert.NoError(AuthReadAsyncReplyByUUID(ctx))

	RegisterSecurityModule(nil)

}
