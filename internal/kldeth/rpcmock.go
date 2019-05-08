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

package kldeth

import (
	"context"
)

// MockRPCSubscription allows convenient subscription mocking in other packages
type MockRPCSubscription struct {
	Namespace  string
	Args       []interface{}
	ErrChan    chan error
	MsgChan    chan<- interface{}
	Subscribed bool
}

// Err returns the configured mock error channel
func (ms *MockRPCSubscription) Err() <-chan error {
	return ms.ErrChan
}

// Unsubscribe captures the unsubscribe call
func (ms *MockRPCSubscription) Unsubscribe() {
	ms.Subscribed = false
}

// MockRPCClient implements RPCClientAll
type MockRPCClient struct {
	// Requests for behaviour
	callError     error
	resultWranger func(string, interface{}, ...interface{})
	// Captured results
	SubscribeErr  error
	SubResult     *MockRPCSubscription
	MethodCapture string
	ArgsCapture   []interface{}
	Closed        bool
}

// NewMockRPCClientForAsync contructs a mock client for async testing
func NewMockRPCClientForAsync(subscribeErr error) *MockRPCClient {
	m := &MockRPCClient{
		SubscribeErr: subscribeErr,
		SubResult: &MockRPCSubscription{
			Subscribed: true,
			ErrChan:    make(chan error),
		},
	}
	return m
}

// NewMockRPCClientForSync contructs a mock client for sync testing
func NewMockRPCClientForSync(callErr error, resultWranger func(string, interface{}, ...interface{})) *MockRPCClient {
	m := &MockRPCClient{
		callError:     callErr,
		resultWranger: resultWranger,
	}
	return m
}

// CallContext invokes the supplied result wranger, and captures the args
func (m *MockRPCClient) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	m.MethodCapture = method
	m.ArgsCapture = args
	if m.resultWranger != nil {
		m.resultWranger(method, result, args...)
	}
	return m.callError
}

// Subscribe returns the subscription already configured in the mock
func (m *MockRPCClient) Subscribe(ctx context.Context, namespace string, channel interface{}, args ...interface{}) (RPCClientSubscription, error) {
	m.SubResult.Namespace = namespace
	m.SubResult.Args = args
	m.SubResult.MsgChan = channel.(chan interface{})
	return &subWrapper{s: m.SubResult}, m.SubscribeErr
}

// Close captures the fact close was called
func (m *MockRPCClient) Close() {
	m.Closed = true
}
