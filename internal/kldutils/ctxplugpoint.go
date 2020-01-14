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

package kldutils

import (
	"context"
	"fmt"
)

type kldContextKey int

const (
	kldContextKeySystemAuth kldContextKey = iota
	kldContextKeyAccessToken
)

// TestSecurityModule designed for unit testing - does not implement security
type TestSecurityModule struct{}

// VerifyToken checks if a token matches a fixed string
func (sm *TestSecurityModule) VerifyToken(tok string) (interface{}, error) {
	if tok == "testat" {
		return "verified", nil
	}
	return nil, fmt.Errorf("badness")
}

// SecurityModule is a embeddable code plugpoint
type SecurityModule interface {
	// VerifyToken verfies a token and returns a context object to store that will be returned to enforcement points
	VerifyToken(string) (interface{}, error)
}

var securityModule SecurityModule

// RegisterSecurityModule is the plug point to register a security module
func RegisterSecurityModule(sm SecurityModule) {
	securityModule = sm
}

// NewSystemContext creates a system background context
func NewSystemContext() context.Context {
	return context.WithValue(context.Background(), kldContextKeySystemAuth, true)
}

// IsSystemContext checks if a context was created as a system context
func IsSystemContext(ctx context.Context) bool {
	b, ok := ctx.Value(kldContextKeySystemAuth).(bool)
	return ok && b
}

// WithAccessToken adds an access token to a base context
func WithAccessToken(ctx context.Context, token string) (context.Context, error) {
	if securityModule != nil {
		ctxValue, err := securityModule.VerifyToken(token)
		if err != nil {
			return nil, err
		}
		return context.WithValue(ctx, kldContextKeyAccessToken, ctxValue), nil
	}
	return ctx, nil
}

// GetAccessToken extracts a previously stored access token from a context
func GetAccessToken(ctx context.Context) interface{} {
	return ctx.Value(kldContextKeyAccessToken)
}
