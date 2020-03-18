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
	"crypto/ecdsa"
	"math/big"
	"net/url"
	"regexp"
	"strings"

	"github.com/alecthomas/template"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	ecrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
)

const (
	defaultAddressProp    = "address"
	defaultPrivateKeyProp = "privateKey"
)

// hdWalletFromAddressMatcher matches the from syntax for HD-InstanceID-WalletID-INDEX
var hdWalletFromAddressMatcher = regexp.MustCompile("(?i)^hd-([^-]+)-([^-]+)-(\\d+)$")

// HDWalletConf configuration
type HDWalletConf struct {
	kldutils.HTTPRequesterConf
	// URLTemplate is a go template such as: "https://someconstant-{{.InstanceID}}/api/v1/{{.WalletID}}/{{.Index}}"
	URLTemplate string                `json:"urlTemplate"`
	ChainID     string                `json:"chainID"`
	PropNames   HDWalletConfPropNames `json:"propNames"`
}

// HDWalletConfPropNames prop names for processing JSON responses
type HDWalletConfPropNames struct {
	Address    string `json:"address"`
	PrivateKey string `json:"privateKey"`
}

type hdWallet struct {
	conf        *HDWalletConf
	urlTemplate *template.Template
	chainID     big.Int
	hr          *kldutils.HTTPRequester
}

// HDWalletRequest is the struct that is extracted from a specially formatted 'from' string, by IsHDWalletRequest
type HDWalletRequest struct {
	InstanceID string
	WalletID   string
	Index      string
}

// HDWallet interface
type HDWallet interface {
	SignerFor(request *HDWalletRequest) (kldeth.TXSigner, error)
}

type hdwalletSigner struct {
	address common.Address
	key     *ecdsa.PrivateKey
	chainID *big.Int
}

// newHDWallet construtor
func newHDWallet(conf *HDWalletConf) HDWallet {
	hd := &hdWallet{
		conf:        conf,
		urlTemplate: template.Must(template.New("urlTemplate").Parse(conf.URLTemplate)),
		hr:          kldutils.NewHTTPRequester("HDWallet", &conf.HTTPRequesterConf),
	}
	propNames := &conf.PropNames
	if propNames.Address == "" {
		propNames.Address = defaultAddressProp
	}
	if propNames.PrivateKey == "" {
		propNames.PrivateKey = defaultPrivateKeyProp
	}
	hd.chainID.SetString(conf.ChainID, 0)
	return hd
}

// IsHDWalletRequest validates a from address to see if it is a HD wallet signing request
func IsHDWalletRequest(fromAddr string) *HDWalletRequest {
	if match := hdWalletFromAddressMatcher.FindStringSubmatch(fromAddr); match != nil {
		return &HDWalletRequest{
			InstanceID: url.PathEscape(match[1]),
			WalletID:   url.PathEscape(match[2]),
			Index:      url.PathEscape(match[3]),
		}
	}
	return nil
}

func (hd *hdWallet) SignerFor(request *HDWalletRequest) (kldeth.TXSigner, error) {

	urlStr := &strings.Builder{}
	hd.urlTemplate.Execute(urlStr, request)

	result, err := hd.hr.DoRequest("GET", urlStr.String(), nil)
	if err != nil {
		log.Errorf("HDWallet request failed: %s", err)
		return nil, klderrors.Errorf(klderrors.HDWalletSigningFailed)
	}

	address, err := hd.hr.GetResponseString(result, hd.conf.PropNames.Address, false)
	if err != nil {
		log.Errorf("Missing address in response: %s", err)
		return nil, klderrors.Errorf(klderrors.HDWalletSigningBadData)
	}
	keyStr, ok := result[hd.conf.PropNames.PrivateKey].(string)
	if !ok {
		log.Errorf("Missing entry in response")
		return nil, klderrors.Errorf(klderrors.HDWalletSigningBadData)
	}
	key, err := ecrypto.HexToECDSA(strings.TrimPrefix(keyStr, "0x"))
	if err != nil {
		log.Errorf("Bad hex value in response '%s': %s", keyStr, err)
		return nil, klderrors.Errorf(klderrors.HDWalletSigningBadData)
	}

	return &hdwalletSigner{
		address: common.HexToAddress(address),
		key:     key,
		chainID: &hd.chainID,
	}, nil

}

func (s *hdwalletSigner) Type() string {
	return "HD Wallet"
}

func (s *hdwalletSigner) Address() string {
	return s.address.String()
}

func (s *hdwalletSigner) Sign(tx *types.Transaction) ([]byte, error) {
	ethSigner := types.NewEIP155Signer(s.chainID)
	signedTX, _ := types.SignTx(tx, ethSigner, s.key)
	signedRLP := new(bytes.Buffer)
	signedTX.EncodeRLP(signedRLP)
	return signedRLP.Bytes(), nil
}
