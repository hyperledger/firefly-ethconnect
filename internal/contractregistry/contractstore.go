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
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-openapi/spec"
	lru "github.com/hashicorp/golang-lru"
	log "github.com/sirupsen/logrus"

	ethconnecterrors "github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/kvstore"
	"github.com/hyperledger/firefly-ethconnect/internal/messages"
)

const (
	// DefaultABICacheSize is the number of entries we will hold in a LRU cache for ABIs
	DefaultABICacheSize = 25
)

type StoredABI struct {
	ABIInfo
	DeployMsg *messages.DeployContract `json:"deployMsg"`
}

type ContractResolver interface {
	ResolveContractAddress(registeredName string) (string, error)
	GetContractByAddress(addrHex string) (*ContractInfo, error)
	GetABI(location ABILocation, refresh bool) (deployMsg *DeployContractWithAddress, err error)
	CheckNameAvailable(name string, isRemote bool) error
}

type ContractStore interface {
	ContractResolver
	Init() error
	Close()
	AddContract(addrHexNo0x, abiID, pathName, registerAs string) (*ContractInfo, error)
	AddABI(id string, deployMsg *messages.DeployContract, createdTime time.Time) (*ABIInfo, error)
	AddRemoteInstance(lookupStr, address string) error
	GetLocalABIInfo(abiID string) (*ABIInfo, error)
	ListContracts() ([]messages.TimeSortable, error)
	ListABIs() ([]messages.TimeSortable, error)
}

type ContractStoreConf struct {
	StoragePath  string `json:"storagePath"`
	BaseURL      string `json:"baseURL"`
	ABICacheSize *int   `json:"abiCacheSize"`
}

type contractStore struct {
	conf     *ContractStoreConf
	rr       RemoteRegistry
	db       kvstore.KVStore
	abiCache *lru.Cache
}

const (
	ldbRegisteredNamePrefix  = "registered_name"
	ldbContractAddressPrefix = "contract_address"
	ldbABIIDPrefix           = "abi_id"
)

func NewContractStore(conf *ContractStoreConf, rr RemoteRegistry) ContractStore {
	return &contractStore{
		conf: conf,
		rr:   rr,
	}
}

// ContractInfo is the minimal data structure we keep in memory, indexed by address
// ONLY used for local registry. Remote registry handles its own storage/caching
type ContractInfo struct {
	messages.TimeSorted
	Address      string `json:"address"`
	Path         string `json:"path"`
	ABI          string `json:"abi"`
	SwaggerURL   string `json:"openapi"`
	RegisteredAs string `json:"registeredAs"`
}

// ABIInfo is the minimal data structure we keep in memory, indexed by our own UUID
type ABIInfo struct {
	messages.TimeSorted
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Path            string `json:"path"`
	Deployable      bool   `json:"deployable"`
	SwaggerURL      string `json:"openapi"`
	CompilerVersion string `json:"compilerVersion"`
}

func (i *ContractInfo) GetID() string {
	return i.Address
}

func (i *ABIInfo) GetID() string {
	return i.ID
}

type abiType int

// These values will be saved with existing subscriptions.
// Do not remove or reorder them (only add new ones to the end).
const (
	RemoteGateway abiType = iota
	RemoteInstance
	LocalABI
)

type ABILocation struct {
	ABIType abiType `json:"type"`
	Name    string  `json:"name"`
}

func IsRemote(msg messages.CommonHeaders) bool {
	ctxMap := msg.Context
	if isRemoteGeneric, ok := ctxMap[RemoteRegistryContextKey]; ok {
		if isRemote, ok := isRemoteGeneric.(bool); ok {
			return isRemote
		}
	}
	return false
}

func (cs *contractStore) AddContract(addrHexNo0x, abiID, pathName, registerAs string) (*ContractInfo, error) {
	contractInfo := &ContractInfo{
		Address:      addrHexNo0x,
		ABI:          abiID,
		Path:         "/contracts/" + pathName,
		SwaggerURL:   cs.conf.BaseURL + "/contracts/" + pathName + "?swagger",
		RegisteredAs: registerAs,
		TimeSorted: messages.TimeSorted{
			CreatedISO8601: time.Now().UTC().Format(time.RFC3339),
		},
	}
	if err := cs.storeContractInfo(contractInfo); err != nil {
		return nil, err
	}
	return contractInfo, nil
}

func (cs *contractStore) storeContractInfo(info *ContractInfo) error {
	if err := cs.addToContractNameIndex(info); err != nil {
		return err
	}
	log.Infof("%s: Storing contract instance JSON for address '%s'", info.ABI, info.Address)
	return cs.db.PutJSON(fmt.Sprintf("%s/%s", ldbContractAddressPrefix, info.Address), info)
}

func (cs *contractStore) ResolveContractAddress(registeredName string) (string, error) {
	nameUnescaped, _ := url.QueryUnescape(registeredName)
	key := fmt.Sprintf("%s/%s", ldbRegisteredNamePrefix, nameUnescaped)
	var info ContractInfo
	err := cs.db.GetJSON(key, &info)
	if err == kvstore.ErrorNotFound {
		return "", ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreContractLoad, registeredName)
	} else if err != nil {
		return "", err
	}
	log.Infof("%s -> 0x%s", registeredName, info.Address)
	return info.Address, nil
}

func (cs *contractStore) GetContractByAddress(addrHex string) (*ContractInfo, error) {
	addrHexNo0x := strings.TrimPrefix(strings.ToLower(addrHex), "0x")
	var info ContractInfo
	err := cs.db.GetJSON(fmt.Sprintf("%s/%s", ldbContractAddressPrefix, addrHexNo0x), &info)
	if err == kvstore.ErrorNotFound {
		return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreContractNotFound, addrHexNo0x)
	}
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (cs *contractStore) AddABI(abiID string, deployMsg *messages.DeployContract, createdTime time.Time) (*ABIInfo, error) {
	storedABI := &StoredABI{
		ABIInfo: ABIInfo{
			ID:              abiID,
			Name:            deployMsg.ContractName,
			Description:     deployMsg.Description,
			Deployable:      len(deployMsg.Compiled) > 0,
			CompilerVersion: deployMsg.CompilerVersion,
			Path:            "/abis/" + abiID,
			SwaggerURL:      cs.conf.BaseURL + "/abis/" + abiID + "?swagger",
			TimeSorted: messages.TimeSorted{
				CreatedISO8601: createdTime.UTC().Format(time.RFC3339),
			},
		},
		DeployMsg: deployMsg,
	}
	err := cs.db.PutJSON(fmt.Sprintf("%s/%s", ldbABIIDPrefix, abiID), storedABI)
	if err != nil {
		return nil, err
	}
	return &storedABI.ABIInfo, nil
}

func (cs *contractStore) GetLocalABIInfo(abiID string) (info *ABIInfo, err error) {
	return info, cs.db.GetJSON(fmt.Sprintf("%s/%s", ldbABIIDPrefix, abiID), &info)
}

func (cs *contractStore) GetABI(location ABILocation, refresh bool) (deployMsg *DeployContractWithAddress, err error) {
	if !refresh {
		if cached, ok := cs.abiCache.Get(location); ok {
			result := cached.(*DeployContractWithAddress)
			log.Infof("Loaded contract from cache: %+v", location)
			return result, nil
		}
	}

	switch location.ABIType {
	case RemoteGateway:
		deployMsg = &DeployContractWithAddress{}
		deployMsg.Contract, err = cs.rr.LoadFactoryForGateway(location.Name, refresh)
	case RemoteInstance:
		deployMsg, err = cs.rr.LoadFactoryForInstance(location.Name, refresh)
	case LocalABI:
		deployMsg, err = cs.getDeployContractByABIID(location.Name)
	default:
		panic("unknown ABI type") // should not happen
	}

	if err != nil || deployMsg == nil || deployMsg.Contract == nil {
		return nil, err
	}
	log.Infof("Adding contract to cache: %+v", location)
	cs.abiCache.Add(location, deployMsg)
	return deployMsg, nil
}

func (cs *contractStore) getDeployContractByABIID(abiID string) (*DeployContractWithAddress, error) {
	var storedABI StoredABI
	err := cs.db.GetJSON(fmt.Sprintf("%s/%s", ldbABIIDPrefix, abiID), &storedABI)
	if err == kvstore.ErrorNotFound {
		log.Infof("ABI with ID %s not found locally", abiID)
		return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreABINotFound, abiID)
	}
	if err != nil {
		return nil, err
	}
	return &DeployContractWithAddress{Contract: storedABI.DeployMsg}, nil
}

func (cs *contractStore) migrateFilesToLevelDB() {
	legacyContractMatcher, _ := regexp.Compile(`^contract_([0-9a-z]{40})\.swagger\.json$`)
	instanceMatcher, _ := regexp.Compile(`^contract_([0-9a-z]{40})\.instance\.json$`)
	abiMatcher, _ := regexp.Compile(`^abi_([0-9a-z-]+)\.deploy.json$`)
	files, err := ioutil.ReadDir(cs.conf.StoragePath)
	if err != nil {
		log.Errorf("Failed to read directory %s: %s", cs.conf.StoragePath, err)
		return
	}
	for _, file := range files {
		if !file.IsDir() {
			fileName := file.Name()
			log.Infof("Checking file for LevelDB migration: %s", fileName)
			legacyContractGroups := legacyContractMatcher.FindStringSubmatch(fileName)
			abiGroups := abiMatcher.FindStringSubmatch(fileName)
			instanceGroups := instanceMatcher.FindStringSubmatch(fileName)
			cleanup := false
			if legacyContractGroups != nil {
				cleanup = cs.migrateLegacyContractFile(legacyContractGroups[1], path.Join(cs.conf.StoragePath, fileName), file.ModTime())
			} else if instanceGroups != nil {
				cleanup = cs.migrateContractFile(instanceGroups[1], path.Join(cs.conf.StoragePath, fileName), file.ModTime())
			} else if abiGroups != nil {
				cleanup = cs.migrateABIFile(abiGroups[1], path.Join(cs.conf.StoragePath, fileName), file.ModTime())
			}
			if cleanup {
				cs.cleanupMigratedFile(fileName)
			}
		}
	}
}

func (cs *contractStore) cleanupMigratedFile(fileName string) {
	if err := os.Remove(fileName); err != nil {
		log.Errorf("Failed to clean-up migrated file %s: %s", fileName, err)
	}
}

func (cs *contractStore) Init() (err error) {
	if cs.conf.StoragePath == "" {
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayMissingStoragePath, err)
	}

	cacheSize := DefaultABICacheSize
	if cs.conf.ABICacheSize != nil {
		cacheSize = *cs.conf.ABICacheSize
	}
	if cs.abiCache, err = lru.New(cacheSize); err != nil {
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayResourceErr, err)
	}
	dbPath := path.Join(cs.conf.StoragePath, "contractsdb")
	if cs.db, err = kvstore.NewLDBKeyValueStore(dbPath); err != nil {
		return err
	}
	cs.migrateFilesToLevelDB()
	return cs.rr.Init()
}

func (cs *contractStore) Close() {
	cs.rr.Close()
}

func (cs *contractStore) migrateABIFile(abiID, fileName string, createdTime time.Time) bool {
	log.Infof("Migrating ABI file to LevelDB: %s", fileName)
	abiFile, err := os.OpenFile(fileName, os.O_RDONLY, 0)
	if err != nil {
		log.Errorf("Failed to load ABI info file %s: %s", fileName, err)
		return false
	}
	defer abiFile.Close()
	var msg messages.DeployContract
	err = json.NewDecoder(bufio.NewReader(abiFile)).Decode(&msg)
	if err != nil {
		log.Errorf("Failed to parse ABI info file %s: %s", fileName, err)
		return false
	}
	if _, err := cs.AddABI(abiID, &msg, createdTime); err != nil {
		log.Errorf("Failed to migrate ABI file into LevelDB: %s", err)
		return false
	}
	return true
}

func (cs *contractStore) migrateContractFile(address, fileName string, createdTime time.Time) bool {
	log.Infof("Migrating Contract Info file to LevelDB: %s", fileName)
	contractFile, err := os.OpenFile(fileName, os.O_RDONLY, 0)
	if err != nil {
		log.Errorf("Failed to load contract info file %s: %s", fileName, err)
		return false
	}
	defer contractFile.Close()
	var info ContractInfo
	err = json.NewDecoder(bufio.NewReader(contractFile)).Decode(&info)
	if err != nil {
		log.Errorf("Failed to parse contract info file %s: %s", fileName, err)
		return false
	}
	if err := cs.storeContractInfo(&info); err != nil {
		log.Errorf("Failed to migrate contract file into LevelDB: %s", err)
		return false
	}

	return true
}

func (cs *contractStore) migrateLegacyContractFile(address, fileName string, createdTime time.Time) bool {
	log.Infof("Migrating Legacy Swagger Contract file to LevelDB: %s", fileName)
	swaggerFile, err := os.OpenFile(fileName, os.O_RDONLY, 0)
	if err != nil {
		log.Errorf("Failed to load Swagger file %s: %s", fileName, err)
		return false
	}
	defer swaggerFile.Close()
	var swagger spec.Swagger
	err = json.NewDecoder(bufio.NewReader(swaggerFile)).Decode(&swagger)
	if err != nil {
		log.Errorf("Failed to parse Swagger file %s: %s", fileName, err)
		return false
	}
	if swagger.Info == nil {
		log.Errorf("Failed to migrate invalid Swagger file %s", fileName)
		return false
	}
	var registeredAs string
	if ext, exists := swagger.Info.Extensions["x-firefly-registered-name"]; exists {
		registeredAs = ext.(string)
	}
	if ext, exists := swagger.Info.Extensions["x-firefly-deployment-id"]; exists {
		_, err := cs.AddContract(address, ext.(string), address, registeredAs)
		if err != nil {
			log.Errorf("Failed to write migrated instance file to LevelDB: %s", err)
			return false
		}
	} else {
		log.Warnf("Swagger cannot be migrated due to missing 'x-firefly-deployment-id' extension: %s", fileName)
		return false
	}

	return true
}

func (cs *contractStore) CheckNameAvailable(registerAs string, isRemote bool) error {
	if isRemote {
		msg, err := cs.rr.LoadFactoryForInstance(registerAs, false)
		if err != nil {
			return err
		} else if msg != nil {
			return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayFriendlyNameClash, msg.Address, registerAs)
		}
		return nil
	}
	var info ContractInfo
	err := cs.db.GetJSON(fmt.Sprintf("%s/%s", ldbRegisteredNamePrefix, registerAs), &info)
	if err == nil {
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayFriendlyNameClash, info.Address, registerAs)
	}
	if err != kvstore.ErrorNotFound {
		return err
	}
	return nil
}

func (cs *contractStore) addToContractNameIndex(info *ContractInfo) error {
	if info.RegisteredAs == "" {
		return nil
	}
	// Protect against overwrite
	if err := cs.CheckNameAvailable(info.RegisteredAs, false); err != nil {
		return err
	}
	log.Infof("Registering %s as '%s'", info.Address, info.RegisteredAs)
	return cs.db.PutJSON(fmt.Sprintf("%s/%s", ldbRegisteredNamePrefix, info.RegisteredAs), info)
}

func (cs *contractStore) AddRemoteInstance(lookupStr, address string) error {
	return cs.rr.RegisterInstance(lookupStr, address)
}

func (cs *contractStore) ListContracts() ([]messages.TimeSortable, error) {
	retval := make([]messages.TimeSortable, 0)
	it := cs.db.NewIteratorWithRange(&kvstore.Range{
		Start: []byte(ldbContractAddressPrefix + "/"),
		Limit: []byte(ldbContractAddressPrefix + "0"),
	})
	for it.Next() {
		var info ContractInfo
		if err := it.ValueJSON(&info); err != nil {
			return nil, err
		}
		retval = append(retval, &info)
	}
	sort.Slice(retval, func(i, j int) bool {
		return retval[i].IsLessThan(retval[i], retval[j])
	})
	return retval, nil
}

func (cs *contractStore) ListABIs() ([]messages.TimeSortable, error) {
	retval := make([]messages.TimeSortable, 0)
	it := cs.db.NewIteratorWithRange(&kvstore.Range{
		Start: []byte(ldbABIIDPrefix + "/"),
		Limit: []byte(ldbABIIDPrefix + "0"),
	})
	for it.Next() {
		var info ABIInfo // we only de-serialize the ABIInfo part of the full record
		if err := it.ValueJSON(&info); err != nil {
			return nil, err
		}
		retval = append(retval, &info)
	}
	sort.Slice(retval, func(i, j int) bool {
		return retval[i].IsLessThan(retval[i], retval[j])
	})
	return retval, nil
}
