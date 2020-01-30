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

package kldtx

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"

	ecrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
)

func TestHDWalletDefaults(t *testing.T) {
	assert := assert.New(t)

	hd := newHDWallet(&HDWalletConf{}).(*hdWallet)

	assert.Equal(defaultAddressProp, hd.conf.PropNames.Address)
	assert.Equal(defaultPrivateKeyProp, hd.conf.PropNames.PrivateKey)
}

func TestHDWalletSignOK(t *testing.T) {
	assert := assert.New(t)

	key, _ := ecrypto.GenerateKey()
	addr := ecrypto.PubkeyToAddress(key.PublicKey)

	svr := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		assert.Equal("/testinst/api/v1/testwallet/1234", req.URL.Path)

		res.WriteHeader(200)
		res.Write([]byte(`
    {
      "addr": "` + addr.String() + `",
      "key": "` + hex.EncodeToString(ecrypto.FromECDSA(key)) + `"
    }`))
	}))
	defer svr.Close()

	hdr := IsHDWalletRequest("hd-testinst-testwallet-1234")
	assert.NotNil(hdr)

	hd := newHDWallet(&HDWalletConf{
		URLTemplate: svr.URL + "/{{.InstanceID}}/api/v1/{{.WalletID}}/{{.Index}}",
		ChainID:     "12345",
		PropNames: HDWalletConfPropNames{
			Address:    "addr",
			PrivateKey: "key",
		},
	}).(*hdWallet)

	s, err := hd.SignerFor(hdr)
	assert.NoError(err)

	assert.Equal(addr.String(), s.Address())

	tx := types.NewContractCreation(12345, big.NewInt(0), 0, big.NewInt(0), []byte("hello world"))

	signed, err := s.Sign(tx)
	assert.NoError(err)

	eip155 := types.NewEIP155Signer(big.NewInt(12345))
	tx2 := &types.Transaction{}
	err = tx2.DecodeRLP(rlp.NewStream(bytes.NewReader(signed), 0))
	assert.NoError(err)
	sender, err := eip155.Sender(tx2)
	assert.NoError(err)
	assert.Equal(addr, sender)
}

func TestHDWalletSignerForRequestFail(t *testing.T) {
	assert := assert.New(t)

	svr := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(500)
	}))
	defer svr.Close()

	hdr := IsHDWalletRequest("hd-testinst-testwallet-1234")
	assert.NotNil(hdr)

	hd := newHDWallet(&HDWalletConf{
		URLTemplate: svr.URL,
		ChainID:     "12345",
	}).(*hdWallet)

	_, err := hd.SignerFor(hdr)
	assert.EqualError(err, "HDWallet signing failed")
}

func TestHDWalletSignerForEmptyResponse(t *testing.T) {
	assert := assert.New(t)

	svr := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(200)
		res.Write([]byte(`{}`))
	}))
	defer svr.Close()

	hdr := IsHDWalletRequest("hd-testinst-testwallet-1234")
	assert.NotNil(hdr)

	hd := newHDWallet(&HDWalletConf{
		URLTemplate: svr.URL,
		ChainID:     "12345",
	}).(*hdWallet)

	_, err := hd.SignerFor(hdr)
	assert.EqualError(err, "Unexpected response from HDWallet")
}

func TestHDWalletSignerBadAddress(t *testing.T) {
	assert := assert.New(t)

	svr := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(200)
		res.Write([]byte(`{"address": 12345}`))
	}))
	defer svr.Close()

	hdr := IsHDWalletRequest("hd-testinst-testwallet-1234")
	assert.NotNil(hdr)

	hd := newHDWallet(&HDWalletConf{
		URLTemplate: svr.URL,
		ChainID:     "12345",
	}).(*hdWallet)

	_, err := hd.SignerFor(hdr)
	assert.EqualError(err, "Unexpected response from HDWallet")
}

func TestHDWalletSignerBadKeyType(t *testing.T) {
	assert := assert.New(t)

	svr := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(200)
		res.Write([]byte(`{"address": "0x", "privateKey": 12345}`))
	}))
	defer svr.Close()

	hdr := IsHDWalletRequest("hd-testinst-testwallet-1234")
	assert.NotNil(hdr)

	hd := newHDWallet(&HDWalletConf{
		URLTemplate: svr.URL,
		ChainID:     "12345",
	}).(*hdWallet)

	_, err := hd.SignerFor(hdr)
	assert.EqualError(err, "Unexpected response from HDWallet")
}

func TestHDWalletSignerBadKey(t *testing.T) {
	assert := assert.New(t)

	svr := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(200)
		res.Write([]byte(`{"address": "0x", "privateKey": "0x"}`))
	}))
	defer svr.Close()

	hdr := IsHDWalletRequest("hd-testinst-testwallet-1234")
	assert.NotNil(hdr)

	hd := newHDWallet(&HDWalletConf{
		URLTemplate: svr.URL,
		ChainID:     "12345",
	}).(*hdWallet)

	_, err := hd.SignerFor(hdr)
	assert.EqualError(err, "Unexpected response from HDWallet")
}
