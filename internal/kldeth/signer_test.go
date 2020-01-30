// Copyright 2018, 2019 Kaleido

package kldeth

import "github.com/ethereum/go-ethereum/core/types"

type mockTXSigner struct {
	capturedTX *types.Transaction
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

func (s *mockTXSigner) Sign(tx *types.Transaction) ([]byte, error) {
	s.capturedTX = tx
	return s.signed, s.signErr
}
