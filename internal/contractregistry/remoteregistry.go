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

package contractregistry

import (
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/hyperledger-labs/firefly-ethconnect/internal/errors"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/kvstore"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/messages"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/utils"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"

	log "github.com/sirupsen/logrus"
)

const (
	defaultIDProp            = "id"
	defaultNameProp          = "name"
	defaultABIProp           = "abi"
	defaultBytecodeProp      = "bytecode"
	defaultDevdocProp        = "devdoc"
	defaultDeployableProp    = "deployable"
	defaultAddressProp       = "address"
	RemoteRegistryContextKey = "isRemoteRegistry"
)

type DeployContractWithAddress struct {
	Contract *messages.DeployContract
	Address  string
}

// RemoteRegistry lookup of ABI, ByteCode and DevDocs against a conformant REST API
type RemoteRegistry interface {
	LoadFactoryForGateway(lookupStr string, refresh bool) (*messages.DeployContract, error)
	LoadFactoryForInstance(lookupStr string, refresh bool) (*DeployContractWithAddress, error)
	RegisterInstance(lookupStr, address string) error
	Init() error
	Close()
}

// RemoteRegistryConf configuration
type RemoteRegistryConf struct {
	utils.HTTPRequesterConf
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
		hr:   utils.NewHTTPRequester("Contract registry", &conf.HTTPRequesterConf),
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
	hr   *utils.HTTPRequester
	db   kvstore.KVStore
}

func (rr *remoteRegistry) Init() (err error) {
	if rr.conf.CacheDB != "" {
		if rr.db, err = kvstore.NewLDBKeyValueStore(rr.conf.CacheDB); err != nil {
			return errors.Errorf(errors.RemoteRegistryCacheInit, err)
		}
	}
	return nil
}

func (rr *remoteRegistry) loadFactoryFromURL(baseURL, ns, lookupStr string, refresh bool) (msg *DeployContractWithAddress, err error) {
	safeLookupStr := url.QueryEscape(lookupStr)
	if !refresh {
		msg = rr.loadFactoryFromCacheDB(ns + "/" + safeLookupStr)
		if msg != nil {
			return msg, nil
		}
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
	var abi ethbinding.ABIMarshaling
	err = json.Unmarshal([]byte(abiString), &abi)
	if err != nil {
		log.Errorf("GET %s <-- !Failed to decode ABI: %s\n%s", queryURL, err, abiString)
		return nil, errors.Errorf(errors.RemoteRegistryLookupGenericProcessingFailed)
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
		return nil, errors.Errorf(errors.RemoteRegistryLookupGenericProcessingFailed)
	}
	addr, _ := rr.hr.GetResponseString(jsonRes, rr.conf.PropNames.Address, false)
	msg = &DeployContractWithAddress{
		Contract: &messages.DeployContract{
			TransactionCommon: messages.TransactionCommon{
				RequestCommon: messages.RequestCommon{
					Headers: messages.RequestHeaders{
						CommonHeaders: messages.CommonHeaders{
							ID: idString,
							Context: map[string]interface{}{
								RemoteRegistryContextKey: true,
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

func (rr *remoteRegistry) loadFactoryFromCacheDB(cacheKey string) *DeployContractWithAddress {
	if rr.db == nil {
		return nil
	}
	b, err := rr.db.Get(cacheKey)
	if err != nil {
		return nil
	}
	var msg DeployContractWithAddress
	err = json.Unmarshal(b, &msg)
	if err != nil {
		log.Warnf("Failed to deserialized cached bytes for key %s: %s", cacheKey, err)
		return nil
	}
	return &msg
}

func (rr *remoteRegistry) storeFactoryToCacheDB(cacheKey string, msg *DeployContractWithAddress) {
	if rr.db == nil {
		return
	}
	b, _ := json.Marshal(msg)
	if err := rr.db.Put(cacheKey, b); err != nil {
		log.Warnf("Failed to write bytes to cache for key %s: %s", cacheKey, err)
		return
	}
}

func (rr *remoteRegistry) LoadFactoryForGateway(lookupStr string, refresh bool) (*messages.DeployContract, error) {
	if rr.conf.GatewayURLPrefix == "" {
		return nil, nil
	}
	msg, err := rr.loadFactoryFromURL(rr.conf.GatewayURLPrefix, "gateways", lookupStr, refresh)
	if msg != nil {
		// There is no address on a gateway, so we just return the DeployMsg
		return msg.Contract, err
	}
	return nil, err
}

func (rr *remoteRegistry) LoadFactoryForInstance(lookupStr string, refresh bool) (*DeployContractWithAddress, error) {
	if rr.conf.InstanceURLPrefix == "" {
		return nil, nil
	}
	return rr.loadFactoryFromURL(rr.conf.InstanceURLPrefix, "instances", lookupStr, refresh)
}

func (rr *remoteRegistry) RegisterInstance(lookupStr, address string) error {
	if rr.conf.InstanceURLPrefix == "" {
		return errors.Errorf(errors.RemoteRegistryNotConfigured)
	}
	safeLookupStr := url.QueryEscape(lookupStr)
	requestURL := strings.TrimSuffix(rr.conf.InstanceURLPrefix, "/")
	bodyMap := make(map[string]interface{})
	bodyMap[rr.conf.PropNames.Name] = safeLookupStr
	bodyMap[rr.conf.PropNames.Address] = address
	log.Debugf("Registering contract: %+v", bodyMap)
	_, err := rr.hr.DoRequest("POST", requestURL, bodyMap)
	if err != nil {
		return errors.Errorf(errors.RemoteRegistryRegistrationFailed, err)
	}
	return nil
}

func (rr *remoteRegistry) Close() {
}
