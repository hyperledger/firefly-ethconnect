// Copyright 2018, 2021 Kaleido

package eth

import (
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
)

type mockTXSigner struct {
	capturedTX *ethbinding.Transaction
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

func (s *mockTXSigner) Sign(tx *ethbinding.Transaction) ([]byte, error) {
	s.capturedTX = tx
	return s.signed, s.signErr
}
