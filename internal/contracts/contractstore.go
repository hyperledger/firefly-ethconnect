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

package contracts

import (
	"bufio"
	"encoding/json"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-openapi/spec"
	log "github.com/sirupsen/logrus"

	ethconnecterrors "github.com/hyperledger-labs/firefly-ethconnect/internal/errors"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/messages"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/remoteregistry"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
)

type ContractResolver interface {
	resolveContractAddr(registeredName string) (string, error)
	lookupContractInstance(addrHex string) (*contractInfo, error)
	loadDeployMsgByID(abi string) (*messages.DeployContract, *abiInfo, error)
	checkNameAvailable(name string, isRemote bool) error
	resolveAddressOrName(id string) (deployMsg *messages.DeployContract, registeredName string, info *contractInfo, err error)
}

type ContractStore interface {
	ContractResolver
	storeNewContractInfo(addrHexNo0x, abiID, pathName, registerAs string) (*contractInfo, error)
	buildIndex()
	addFileToContractIndex(address, fileName string)
	addToABIIndex(id string, deployMsg *messages.DeployContract, createdTime time.Time) *abiInfo
	listContracts() []messages.TimeSortable
	listABIs() []messages.TimeSortable
}

type contractStore struct {
	conf                  *SmartContractGatewayConf
	rr                    remoteregistry.RemoteRegistry
	contractIndex         map[string]messages.TimeSortable
	contractRegistrations map[string]*contractInfo
	idxLock               sync.Mutex
	abiIndex              map[string]messages.TimeSortable
}

func newContractStore(conf *SmartContractGatewayConf, rr remoteregistry.RemoteRegistry) ContractStore {
	return &contractStore{
		conf:                  conf,
		rr:                    rr,
		contractIndex:         make(map[string]messages.TimeSortable),
		contractRegistrations: make(map[string]*contractInfo),
		abiIndex:              make(map[string]messages.TimeSortable),
	}
}

// contractInfo is the minimal data structure we keep in memory, indexed by address
// ONLY used for local registry. Remote registry handles its own storage/caching
type contractInfo struct {
	messages.TimeSorted
	Address      string `json:"address"`
	Path         string `json:"path"`
	ABI          string `json:"abi"`
	SwaggerURL   string `json:"openapi"`
	RegisteredAs string `json:"registeredAs"`
}

// abiInfo is the minimal data structure we keep in memory, indexed by our own UUID
type abiInfo struct {
	messages.TimeSorted
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Path            string `json:"path"`
	Deployable      bool   `json:"deployable"`
	SwaggerURL      string `json:"openapi"`
	CompilerVersion string `json:"compilerVersion"`
}

// remoteContractInfo is the ABI raw data back out of the REST API gateway with bytecode
type remoteContractInfo struct {
	ID      string                   `json:"id"`
	Address string                   `json:"address,omitempty"`
	ABI     ethbinding.ABIMarshaling `json:"abi"`
}

func (i *contractInfo) GetID() string {
	return i.Address
}

func (i *abiInfo) GetID() string {
	return i.ID
}

func (g *contractStore) storeNewContractInfo(addrHexNo0x, abiID, pathName, registerAs string) (*contractInfo, error) {
	contractInfo := &contractInfo{
		Address:      addrHexNo0x,
		ABI:          abiID,
		Path:         "/contracts/" + pathName,
		SwaggerURL:   g.conf.BaseURL + "/contracts/" + pathName + "?swagger",
		RegisteredAs: registerAs,
		TimeSorted: messages.TimeSorted{
			CreatedISO8601: time.Now().UTC().Format(time.RFC3339),
		},
	}
	if err := g.storeContractInfo(contractInfo); err != nil {
		return nil, err
	}
	return contractInfo, nil
}

func isRemote(msg messages.CommonHeaders) bool {
	ctxMap := msg.Context
	if isRemoteGeneric, ok := ctxMap[remoteregistry.RemoteRegistryContextKey]; ok {
		if isRemote, ok := isRemoteGeneric.(bool); ok {
			return isRemote
		}
	}
	return false
}

func (g *contractStore) storeContractInfo(info *contractInfo) error {
	if err := g.addToContractIndex(info); err != nil {
		return err
	}
	infoFile := path.Join(g.conf.StoragePath, "contract_"+info.Address+".instance.json")
	instanceBytes, _ := json.MarshalIndent(info, "", "  ")
	log.Infof("%s: Storing contract instance JSON to '%s'", info.ABI, infoFile)
	if err := ioutil.WriteFile(infoFile, instanceBytes, 0664); err != nil {
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreContractSave, err)
	}
	return nil
}

func (g *contractStore) resolveContractAddr(registeredName string) (string, error) {
	nameUnescaped, _ := url.QueryUnescape(registeredName)
	info, exists := g.contractRegistrations[nameUnescaped]
	if !exists {
		return "", ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreContractLoad, registeredName)
	}
	log.Infof("%s -> 0x%s", registeredName, info.Address)
	return info.Address, nil
}

func (g *contractStore) lookupContractInstance(addrHex string) (*contractInfo, error) {
	addrHexNo0x := strings.TrimPrefix(strings.ToLower(addrHex), "0x")
	info, exists := g.contractIndex[addrHexNo0x]
	if !exists {
		return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreContractNotFound, addrHexNo0x)
	}
	return info.(*contractInfo), nil
}

func (g *contractStore) loadDeployMsgByID(id string) (*messages.DeployContract, *abiInfo, error) {
	var info *abiInfo
	var msg *messages.DeployContract
	ts, exists := g.abiIndex[id]
	if !exists {
		log.Infof("ABI with ID %s not found locally", id)
		return nil, nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreABINotFound, id)
	}
	deployFile := path.Join(g.conf.StoragePath, "abi_"+id+".deploy.json")
	deployBytes, err := ioutil.ReadFile(deployFile)
	if err != nil {
		return nil, nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreABILoad, id, err)
	}
	msg = &messages.DeployContract{}
	if err = json.Unmarshal(deployBytes, msg); err != nil {
		return nil, nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreABIParse, id, err)
	}
	info = ts.(*abiInfo)
	return msg, info, nil
}

func (g *contractStore) buildIndex() {
	log.Infof("Building installed smart contract index")
	legacyContractMatcher, _ := regexp.Compile("^contract_([0-9a-z]{40})\\.swagger\\.json$")
	instanceMatcher, _ := regexp.Compile("^contract_([0-9a-z]{40})\\.instance\\.json$")
	abiMatcher, _ := regexp.Compile("^abi_([0-9a-z-]+)\\.deploy.json$")
	files, err := ioutil.ReadDir(g.conf.StoragePath)
	if err != nil {
		log.Errorf("Failed to read directory %s: %s", g.conf.StoragePath, err)
		return
	}
	for _, file := range files {
		fileName := file.Name()
		legacyContractGroups := legacyContractMatcher.FindStringSubmatch(fileName)
		abiGroups := abiMatcher.FindStringSubmatch(fileName)
		instanceGroups := instanceMatcher.FindStringSubmatch(fileName)
		if legacyContractGroups != nil {
			g.migrateLegacyContract(legacyContractGroups[1], path.Join(g.conf.StoragePath, fileName), file.ModTime())
		} else if instanceGroups != nil {
			g.addFileToContractIndex(instanceGroups[1], path.Join(g.conf.StoragePath, fileName))
		} else if abiGroups != nil {
			g.addFileToABIIndex(abiGroups[1], path.Join(g.conf.StoragePath, fileName), file.ModTime())
		}
	}
	log.Infof("Smart contract index built. %d entries", len(g.contractIndex))
}

func (g *contractStore) migrateLegacyContract(address, fileName string, createdTime time.Time) {
	swaggerFile, err := os.OpenFile(fileName, os.O_RDONLY, 0)
	if err != nil {
		log.Errorf("Failed to load Swagger file %s: %s", fileName, err)
		return
	}
	defer swaggerFile.Close()
	var swagger spec.Swagger
	err = json.NewDecoder(bufio.NewReader(swaggerFile)).Decode(&swagger)
	if err != nil {
		log.Errorf("Failed to parse Swagger file %s: %s", fileName, err)
		return
	}
	if swagger.Info == nil {
		log.Errorf("Failed to migrate invalid Swagger file %s", fileName)
		return
	}
	var registeredAs string
	if ext, exists := swagger.Info.Extensions["x-firefly-registered-name"]; exists {
		registeredAs = ext.(string)
	}
	if ext, exists := swagger.Info.Extensions["x-firefly-deployment-id"]; exists {
		_, err := g.storeNewContractInfo(address, ext.(string), address, registeredAs)
		if err != nil {
			log.Errorf("Failed to write migrated instance file: %s", err)
			return
		}

		if err := os.Remove(fileName); err != nil {
			log.Errorf("Failed to clean-up migrated file %s: %s", fileName, err)
		}

	} else {
		log.Warnf("Swagger cannot be migrated due to missing 'x-firefly-deployment-id' extension: %s", fileName)
	}

}

func (g *contractStore) addFileToContractIndex(address, fileName string) {
	contractFile, err := os.OpenFile(fileName, os.O_RDONLY, 0)
	if err != nil {
		log.Errorf("Failed to load contract instance file %s: %s", fileName, err)
		return
	}
	defer contractFile.Close()
	var contractInfo contractInfo
	err = json.NewDecoder(bufio.NewReader(contractFile)).Decode(&contractInfo)
	if err != nil {
		log.Errorf("Failed to parse contract instnace deployment file %s: %s", fileName, err)
		return
	}
	g.addToContractIndex(&contractInfo)
}

func (g *contractStore) addFileToABIIndex(id, fileName string, createdTime time.Time) {
	deployFile, err := os.OpenFile(fileName, os.O_RDONLY, 0)
	if err != nil {
		log.Errorf("Failed to load ABI deployment file %s: %s", fileName, err)
		return
	}
	defer deployFile.Close()
	var deployMsg messages.DeployContract
	err = json.NewDecoder(bufio.NewReader(deployFile)).Decode(&deployMsg)
	if err != nil {
		log.Errorf("Failed to parse ABI deployment file %s: %s", fileName, err)
		return
	}
	g.addToABIIndex(id, &deployMsg, createdTime)
}

func (g *contractStore) checkNameAvailable(registerAs string, isRemote bool) error {
	if isRemote {
		msg, err := g.rr.LoadFactoryForInstance(registerAs, false)
		if err != nil {
			return err
		} else if msg != nil {
			return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayFriendlyNameClash, msg.Address, registerAs)
		}
		return nil
	}
	if existing, exists := g.contractRegistrations[registerAs]; exists {
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayFriendlyNameClash, existing.Address, registerAs)
	}
	return nil
}

func (g *contractStore) addToContractIndex(info *contractInfo) error {
	g.idxLock.Lock()
	defer g.idxLock.Unlock()
	if info.RegisteredAs != "" {
		// Protect against overwrite
		if err := g.checkNameAvailable(info.RegisteredAs, false); err != nil {
			return err
		}
		log.Infof("Registering %s as '%s'", info.Address, info.RegisteredAs)
		g.contractRegistrations[info.RegisteredAs] = info
	}
	g.contractIndex[info.Address] = info
	return nil
}

func (g *contractStore) addToABIIndex(id string, deployMsg *messages.DeployContract, createdTime time.Time) *abiInfo {
	g.idxLock.Lock()
	info := &abiInfo{
		ID:              id,
		Name:            deployMsg.ContractName,
		Description:     deployMsg.Description,
		Deployable:      len(deployMsg.Compiled) > 0,
		CompilerVersion: deployMsg.CompilerVersion,
		Path:            "/abis/" + id,
		SwaggerURL:      g.conf.BaseURL + "/abis/" + id + "?swagger",
		TimeSorted: messages.TimeSorted{
			CreatedISO8601: createdTime.UTC().Format(time.RFC3339),
		},
	}
	g.abiIndex[id] = info
	g.idxLock.Unlock()
	return info
}

func (g *contractStore) listContracts() []messages.TimeSortable {
	g.idxLock.Lock()
	retval := make([]messages.TimeSortable, 0, len(g.contractIndex))
	for _, info := range g.contractIndex {
		retval = append(retval, info)
	}
	g.idxLock.Unlock()
	return retval
}

func (g *contractStore) listABIs() []messages.TimeSortable {
	g.idxLock.Lock()
	retval := make([]messages.TimeSortable, 0, len(g.abiIndex))
	for _, info := range g.abiIndex {
		retval = append(retval, info)
	}
	g.idxLock.Unlock()
	return retval
}

func (g *contractStore) resolveAddressOrName(id string) (deployMsg *messages.DeployContract, registeredName string, info *contractInfo, err error) {
	info, err = g.lookupContractInstance(id)
	if err != nil {
		var origErr = err
		registeredName = id
		if id, err = g.resolveContractAddr(registeredName); err != nil {
			log.Infof("%s is not a friendly name: %s", registeredName, err)
			return nil, "", nil, origErr
		}
		if info, err = g.lookupContractInstance(id); err != nil {
			return nil, "", nil, err
		}
	}
	deployMsg, _, err = g.loadDeployMsgByID(info.ABI)
	return deployMsg, registeredName, info, err
}
