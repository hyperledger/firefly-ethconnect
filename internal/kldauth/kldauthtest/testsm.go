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

package kldauthtest

import (
	"fmt"
)

// TestSecurityModule designed for unit testing - does not implement security
type TestSecurityModule struct{}

// VerifyToken of TEST MODULE checks if a token matches a fixed string
func (sm *TestSecurityModule) VerifyToken(tok string) (interface{}, error) {
	if tok == "testat" {
		return "verified", nil
	}
	return nil, fmt.Errorf("badness")
}

// AuthRPC of TEST MODULE checks if a token matches a fixed string
func (sm *TestSecurityModule) AuthRPC(authCtx interface{}, method string, args ...interface{}) error {
	switch authCtx.(type) {
	case string:
		if method == "testrpc" {
			return nil
		}
	}
	return fmt.Errorf("badness")
}

// AuthRPCSubscribe of TEST MODULE checks if a namespace matches a fixed string
func (sm *TestSecurityModule) AuthRPCSubscribe(authCtx interface{}, namespace string, channel interface{}, args ...interface{}) error {
	switch authCtx.(type) {
	case string:
		if namespace == "testns" {
			return nil
		}
	}
	return fmt.Errorf("badness")
}

// AuthEventStreams of TEST MODULE returns true if there is an auth context
func (sm *TestSecurityModule) AuthEventStreams(authCtx interface{}) error {
	switch authCtx.(type) {
	case string:
		return nil
	}
	return fmt.Errorf("badness")
}

// AuthListAsyncReplies of TEST MODULE returns true if there is an auth context
func (sm *TestSecurityModule) AuthListAsyncReplies(authCtx interface{}) error {
	switch authCtx.(type) {
	case string:
		return nil
	}
	return fmt.Errorf("badness")
}

// AuthReadAsyncReplyByUUID of TEST MODULE returns true if there is an auth context
func (sm *TestSecurityModule) AuthReadAsyncReplyByUUID(authCtx interface{}) error {
	switch authCtx.(type) {
	case string:
		return nil
	}
	return fmt.Errorf("badness")
}
