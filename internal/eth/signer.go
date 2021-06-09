// Copyright 2018, 2021 Kaleido

package eth

import (
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
)

// TXSigner is an interface for pre-signing signing using the parameters of eth_sendTransaction
type TXSigner interface {
	Type() string
	Address() string
	Sign(tx *ethbinding.Transaction) ([]byte, error)
}
