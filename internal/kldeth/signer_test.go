// Copyright 2018, 2019 Kaleido

package kldeth

import "github.com/kaleido-io/ethconnect/internal/kldbind"

type mockTXSigner struct {
	capturedTX *kldbind.Transaction
	from       string
	signed     []byte
	signErr    error
}

func (s *mockTXSigner) Type() string {
	return "mock signer"
}

func (s *mockTXSigner) Address() string {
	return s.from
}

func (s *mockTXSigner) Sign(tx *kldbind.Transaction) ([]byte, error) {
	s.capturedTX = tx
	return s.signed, s.signErr
}
