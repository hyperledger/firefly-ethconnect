// Copyright 2018, 2019 Kaleido

package kldeth

import "github.com/ethereum/go-ethereum/core/types"

// TXSigner is an interface for pre-signing signing using the parameters of eth_sendTransaction
type TXSigner interface {
	Type() string
	Address() string
	Sign(tx *types.Transaction) ([]byte, error)
}
