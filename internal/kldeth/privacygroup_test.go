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

package kldeth

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestGetOrionPrivacyGroupExists(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	firstCall := true
	r := testRPCClient{
		resultWrangler: func(retString interface{}) {
			if firstCall {
				retVal := []OrionPrivacyGroup{
					OrionPrivacyGroup{
						PrivacyGroupID: "P8SxRUussJKqZu4+nUkMJpscQeWOR3HqbAXLakatsk8=",
					},
				}
				reflect.ValueOf(retString).Elem().Set(reflect.ValueOf(retVal))
			}
			firstCall = false
		},
	}

	addr := common.HexToAddress("0xD50ce736021D9F7B0B2566a3D2FA7FA3136C003C")
	privacyGroupID, err := GetOrionPrivacyGroup(context.Background(), &r, &addr,
		"jO6dpqnMhmnrCHqUumyK09+18diF7quq/rROGs2HFWI=",
		[]string{"2QiZG7rYPzRvRsioEn6oYUff1DOvPA22EZr0+/o3RUg="})

	assert.Equal(nil, err)
	assert.Equal("P8SxRUussJKqZu4+nUkMJpscQeWOR3HqbAXLakatsk8=", privacyGroupID)
	assert.Equal("priv_findPrivacyGroup", r.capturedMethod)
	assert.Empty(r.capturedMethod2)
}

func TestGetOrionPrivacyGroupDoesNotExist(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{}

	addr := common.HexToAddress("0xD50ce736021D9F7B0B2566a3D2FA7FA3136C003C")
	_, err := GetOrionPrivacyGroup(context.Background(), &r, &addr,
		"jO6dpqnMhmnrCHqUumyK09+18diF7quq/rROGs2HFWI=",
		[]string{"2QiZG7rYPzRvRsioEn6oYUff1DOvPA22EZr0+/o3RUg="})

	assert.Equal(nil, err)
	assert.Equal("priv_findPrivacyGroup", r.capturedMethod)
	assert.Equal("priv_createPrivacyGroup", r.capturedMethod2)
}

func TestGetOrionPrivacyGroupErrFind(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{
		mockError: fmt.Errorf("pop"),
	}

	addr := common.HexToAddress("0xD50ce736021D9F7B0B2566a3D2FA7FA3136C003C")
	_, err := GetOrionPrivacyGroup(context.Background(), &r, &addr,
		"jO6dpqnMhmnrCHqUumyK09+18diF7quq/rROGs2HFWI=",
		[]string{"2QiZG7rYPzRvRsioEn6oYUff1DOvPA22EZr0+/o3RUg="})

	assert.EqualError(err, "priv_findPrivacyGroup returned: pop")
}

func TestGetOrionPrivacyGroupErrCreate(t *testing.T) {
	log.SetLevel(log.DebugLevel)
	assert := assert.New(t)

	r := testRPCClient{
		mockError2: fmt.Errorf("pop"),
	}

	addr := common.HexToAddress("0xD50ce736021D9F7B0B2566a3D2FA7FA3136C003C")
	_, err := GetOrionPrivacyGroup(context.Background(), &r, &addr,
		"jO6dpqnMhmnrCHqUumyK09+18diF7quq/rROGs2HFWI=",
		[]string{"2QiZG7rYPzRvRsioEn6oYUff1DOvPA22EZr0+/o3RUg="})

	assert.EqualError(err, "priv_createPrivacyGroup returned: pop")
}
