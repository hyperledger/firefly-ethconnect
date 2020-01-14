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

package kldutils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemContext(t *testing.T) {
	assert := assert.New(t)

	assert.True(IsSystemContext(NewSystemContext()))
	assert.False(IsSystemContext(context.Background()))
	assert.False(IsSystemContext(context.WithValue(context.Background(), kldContextKeySystemAuth, "false")))
	assert.False(IsSystemContext(context.WithValue(context.Background(), kldContextKeySystemAuth, false)))

}

func TestAccessToken(t *testing.T) {
	assert := assert.New(t)

	ctx, err := WithAccessToken(context.Background(), "testat")
	assert.NoError(err)
	assert.Equal(nil, GetAccessToken(ctx))

	RegisterSecurityModule(&TestSecurityModule{})

	ctx, err = WithAccessToken(context.Background(), "testat")
	assert.NoError(err)
	assert.Equal("verified", GetAccessToken(ctx))
	assert.Equal(nil, GetAccessToken(context.Background()))

	ctx, err = WithAccessToken(context.Background(), "badone")
	assert.EqualError(err, "badness")

	RegisterSecurityModule(nil)
}
