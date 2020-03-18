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

package kldcontracts

import (
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldkvstore"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"

	log "github.com/sirupsen/logrus"
)

const (
	defaultIDProp         = "id"
	defaultNameProp       = "name"
	defaultABIProp        = "abi"
	defaultBytecodeProp   = "bytecode"
	defaultDevdocProp     = "devdoc"
	defaultDeployableProp = "deployable"
	defaultAddressProp    = "address"
)

type deployContractWithAddress struct {
	kldmessages.DeployContract
	Address string `json:"address"`
}

// RemoteRegistry lookup of ABI, ByteCode and DevDocs against a conformant REST API
type RemoteRegistry interface {
	loadFactoryForGateway(lookupStr string) (*kldmessages.DeployContract, error)
	loadFactoryForInstance(lookupStr string) (*deployContractWithAddress, error)
	registerInstance(lookupStr, address string) error
	init() error
	close()
}

// RemoteRegistryConf configuration
type RemoteRegistryConf struct {
	kldutils.HTTPRequesterConf
	CacheDB           string                      `json:"cacheDB"`
	GatewayURLPrefix  string                      `json:"gatewayURLPrefix"`
	InstanceURLPrefix string                      `json:"instanceURLPrefix"`
	PropNames         RemoteRegistryPropNamesConf `json:"propNames"`
}

// RemoteRegistryPropNamesConf configures the JSON property names to extract from the GET response on the API
type RemoteRegistryPropNamesConf struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ABI        string `json:"abi"`
	Bytecode   string `json:"bytecode"`
	Devdoc     string `json:"devdoc"`
	Deployable string `json:"deployable"`
	Address    string `json:"address"`
}

// NewRemoteRegistry construtor
func NewRemoteRegistry(conf *RemoteRegistryConf) RemoteRegistry {
	rr := &remoteRegistry{
		conf: conf,
		hr:   kldutils.NewHTTPRequester("Contract registry", &conf.HTTPRequesterConf),
	}
	propNames := &conf.PropNames
	if propNames.ID == "" {
		propNames.ID = defaultIDProp
	}
	if propNames.Name == "" {
		propNames.Name = defaultNameProp
	}
	if propNames.ABI == "" {
		propNames.ABI = defaultABIProp
	}
	if propNames.Bytecode == "" {
		propNames.Bytecode = defaultBytecodeProp
	}
	if propNames.Devdoc == "" {
		propNames.Devdoc = defaultDevdocProp
	}
	if propNames.Deployable == "" {
		propNames.Deployable = defaultDeployableProp
	}
	if propNames.Address == "" {
		propNames.Address = defaultAddressProp
	}
	if rr.conf.GatewayURLPrefix != "" && !strings.HasSuffix(rr.conf.GatewayURLPrefix, "/") {
		rr.conf.GatewayURLPrefix += "/"
	}
	if rr.conf.InstanceURLPrefix != "" && !strings.HasSuffix(rr.conf.InstanceURLPrefix, "/") {
		rr.conf.InstanceURLPrefix += "/"
	}
	return rr
}

type remoteRegistry struct {
	conf *RemoteRegistryConf
	hr   *kldutils.HTTPRequester
	db   kldkvstore.KVStore
}

func (rr *remoteRegistry) init() (err error) {
	if rr.conf.CacheDB != "" {
		if rr.db, err = kldkvstore.NewLDBKeyValueStore(rr.conf.CacheDB); err != nil {
			return klderrors.Errorf(klderrors.RemoteRegistryCacheInit, err)
		}
	}
	return nil
}

func (rr *remoteRegistry) loadFactoryFromURL(baseURL, ns, lookupStr string) (*deployContractWithAddress, error) {
	safeLookupStr := url.QueryEscape(lookupStr)
	msg := rr.loadFactoryFromCacheDB(ns + "/" + safeLookupStr)
	if msg != nil {
		return msg, nil
	}
	queryURL := baseURL + safeLookupStr
	jsonRes, err := rr.hr.DoRequest("GET", queryURL, nil)
	if err != nil || jsonRes == nil {
		return nil, err
	}
	idString, err := rr.hr.GetResponseString(jsonRes, rr.conf.PropNames.ID, false)
	if err != nil {
		return nil, err
	}
	abiString, err := rr.hr.GetResponseString(jsonRes, rr.conf.PropNames.ABI, false)
	if err != nil {
		return nil, err
	}
	var abi *kldbind.ABI
	err = json.Unmarshal([]byte(abiString), &abi)
	if err != nil {
		log.Errorf("GET %s <-- !Failed to decode ABI: %s\n%s", queryURL, err, abiString)
		return nil, klderrors.Errorf(klderrors.RemoteRegistryLookupGenericProcessingFailed)
	}
	devdoc, err := rr.hr.GetResponseString(jsonRes, rr.conf.PropNames.Devdoc, true)
	if err != nil {
		return nil, err
	}
	bytecodeStr, err := rr.hr.GetResponseString(jsonRes, rr.conf.PropNames.Bytecode, false)
	if err != nil {
		return nil, err
	}
	var bytecode []byte
	if bytecode, err = hex.DecodeString(strings.TrimPrefix(bytecodeStr, "0x")); err != nil {
		log.Errorf("GET %s <-- !Failed to parse bytecode: %s\n%s", queryURL, err, bytecodeStr)
		return nil, klderrors.Errorf(klderrors.RemoteRegistryLookupGenericProcessingFailed)
	}
	addr, _ := rr.hr.GetResponseString(jsonRes, rr.conf.PropNames.Address, false)
	msg = &deployContractWithAddress{
		DeployContract: kldmessages.DeployContract{
			TransactionCommon: kldmessages.TransactionCommon{
				RequestCommon: kldmessages.RequestCommon{
					Headers: kldmessages.RequestHeaders{
						CommonHeaders: kldmessages.CommonHeaders{
							ID: idString,
							Context: map[string]interface{}{
								remoteRegistryContextKey: true,
							},
						},
					},
				},
			},
			ABI:      abi,
			DevDoc:   devdoc,
			Compiled: bytecode,
		},
		Address: strings.ToLower(strings.TrimPrefix(addr, "0x")),
	}
	rr.storeFactoryToCacheDB(ns+"/"+safeLookupStr, msg)
	return msg, nil
}

func (rr *remoteRegistry) loadFactoryFromCacheDB(cacheKey string) *deployContractWithAddress {
	if rr.db == nil {
		return nil
	}
	b, err := rr.db.Get(cacheKey)
	if err != nil {
		return nil
	}
	var msg deployContractWithAddress
	err = json.Unmarshal(b, &msg)
	if err != nil {
		log.Warnf("Failed to deserialized cached bytes for key %s: %s", cacheKey, err)
		return nil
	}
	return &msg
}

func (rr *remoteRegistry) storeFactoryToCacheDB(cacheKey string, msg *deployContractWithAddress) {
	if rr.db == nil {
		return
	}
	b, _ := json.Marshal(msg)
	if err := rr.db.Put(cacheKey, b); err != nil {
		log.Warnf("Failed to write bytes to cache for key %s: %s", cacheKey, err)
		return
	}
}

func (rr *remoteRegistry) loadFactoryForGateway(lookupStr string) (*kldmessages.DeployContract, error) {
	if rr.conf.GatewayURLPrefix == "" {
		return nil, nil
	}
	msg, err := rr.loadFactoryFromURL(rr.conf.GatewayURLPrefix, "gateways", lookupStr)
	if msg != nil {
		// There is no address on a gateway, so we just return the DeployMsg
		return &msg.DeployContract, err
	}
	return nil, err
}

func (rr *remoteRegistry) loadFactoryForInstance(lookupStr string) (*deployContractWithAddress, error) {
	if rr.conf.InstanceURLPrefix == "" {
		return nil, nil
	}
	return rr.loadFactoryFromURL(rr.conf.InstanceURLPrefix, "instances", lookupStr)
}

func (rr *remoteRegistry) registerInstance(lookupStr, address string) error {
	if rr.conf.InstanceURLPrefix == "" {
		return klderrors.Errorf(klderrors.RemoteRegistryNotConfigured)
	}
	safeLookupStr := url.QueryEscape(lookupStr)
	requestURL := strings.TrimSuffix(rr.conf.InstanceURLPrefix, "/")
	bodyMap := make(map[string]interface{})
	bodyMap[rr.conf.PropNames.Name] = safeLookupStr
	bodyMap[rr.conf.PropNames.Address] = address
	log.Debugf("Registering contract: %+v", bodyMap)
	_, err := rr.hr.DoRequest("POST", requestURL, bodyMap)
	if err != nil {
		return klderrors.Errorf(klderrors.RemoteRegistryRegistrationFailed, err)
	}
	return nil
}

func (rr *remoteRegistry) close() {
}
