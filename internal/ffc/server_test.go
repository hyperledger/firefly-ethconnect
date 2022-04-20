// Copyright © 2022 Kaleido, Inc.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ffc

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/hyperledger/firefly-ethconnect/mocks/ethmocks"
	"github.com/hyperledger/firefly-transaction-manager/pkg/ffcapi"
	"github.com/hyperledger/firefly/pkg/fftypes"
	"github.com/stretchr/testify/assert"
)

func newTestFFCAPIServer() (*ffcServer, *ethmocks.RPCClient) {
	mRpc := &ethmocks.RPCClient{}
	return NewFFCServer(mRpc).(*ffcServer), mRpc
}

func TestServerBadVersion(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	recorder := httptest.NewRecorder()
	s.ServeFFCAPI(context.Background(), &ffcapi.Header{
		Version: ffcapi.Version("not a sem ver"),
	}, []byte{}, recorder)

	assert.Equal(t, recorder.Result().StatusCode, 400)
	var errRes ffcapi.ErrorResponse
	err := json.NewDecoder(recorder.Body).Decode(&errRes)
	assert.NoError(t, err)
	assert.Regexp(t, "FFEC100208", errRes.Error)
	assert.Regexp(t, ffcapi.ErrorReasonInvalidInputs, errRes.Reason)

}

func TestServerIncompatibleVersion(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	recorder := httptest.NewRecorder()
	s.ServeFFCAPI(context.Background(), &ffcapi.Header{
		Version: ffcapi.Version("v99.0.0"),
	}, []byte{}, recorder)

	assert.Equal(t, recorder.Result().StatusCode, 400)
	var errRes ffcapi.ErrorResponse
	err := json.NewDecoder(recorder.Body).Decode(&errRes)
	assert.NoError(t, err)
	assert.Regexp(t, "FFEC100209", errRes.Error)
	assert.Regexp(t, ffcapi.ErrorReasonInvalidInputs, errRes.Reason)

}

func TestServerMissingID(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	recorder := httptest.NewRecorder()
	s.ServeFFCAPI(context.Background(), &ffcapi.Header{
		Version: ffcapi.Version("v1.0.1"),
	}, []byte{}, recorder)

	assert.Equal(t, recorder.Result().StatusCode, 400)
	var errRes ffcapi.ErrorResponse
	err := json.NewDecoder(recorder.Body).Decode(&errRes)
	assert.NoError(t, err)
	assert.Regexp(t, "FFEC100211", errRes.Error)
	assert.Regexp(t, ffcapi.ErrorReasonInvalidInputs, errRes.Reason)

}

func TestServerUnknownRequestType(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	recorder := httptest.NewRecorder()
	s.ServeFFCAPI(context.Background(), &ffcapi.Header{
		Version:     ffcapi.Version("v1.0.1"),
		RequestID:   fftypes.NewUUID(),
		RequestType: "test",
	}, []byte{}, recorder)

	assert.Equal(t, recorder.Result().StatusCode, 400)
	var errRes ffcapi.ErrorResponse
	err := json.NewDecoder(recorder.Body).Decode(&errRes)
	assert.NoError(t, err)
	assert.Regexp(t, "FFEC100210", errRes.Error)
	assert.Regexp(t, ffcapi.ErrorReasonInvalidInputs, errRes.Reason)

}

func TestServerUnknownRequestOK(t *testing.T) {

	s, _ := newTestFFCAPIServer()
	s.handlerMap[ffcapi.RequestType("test")] = func(ctx context.Context, payload []byte) (res interface{}, reason ffcapi.ErrorReason, err error) {
		return map[string]interface{}{
			"test": "data",
		}, "", nil
	}
	recorder := httptest.NewRecorder()
	s.ServeFFCAPI(context.Background(), &ffcapi.Header{
		Version:     ffcapi.Version("v1.0.1"),
		RequestID:   fftypes.NewUUID(),
		RequestType: "test",
	}, []byte{}, recorder)

	assert.Equal(t, recorder.Result().StatusCode, 200)
	var mapRes map[string]interface{}
	err := json.NewDecoder(recorder.Body).Decode(&mapRes)
	assert.NoError(t, err)
	assert.Regexp(t, "data", mapRes["test"])

}

func TestMapReasonStatus(t *testing.T) {
	s, _ := newTestFFCAPIServer()
	assert.Equal(t, 404, s.mapReasonStatus(ffcapi.ErrorReasonNotFound))
	assert.Equal(t, 400, s.mapReasonStatus(ffcapi.ErrorReasonInvalidInputs))
	assert.Equal(t, 500, s.mapReasonStatus(""))
}
