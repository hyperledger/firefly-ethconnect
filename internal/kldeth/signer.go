// Copyright 2018, 2019 Kaleido

package kldeth

import "github.com/kaleido-io/ethbinding"

// TXSigner is an interface for pre-signing signing using the parameters of eth_sendTransaction
type TXSigner interface {
	Type() string
	Address() string
	Sign(tx *ethbinding.Transaction) ([]byte, error)
}
