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
)

type ContractResolver interface {
	ResolveContractAddr(registeredName string) (string, error)
	LookupContractInstance(addrHex string) (*ContractInfo, error)
	LoadDeployMsgByID(abi string) (*messages.DeployContract, *ABIInfo, error)
	CheckNameAvailable(name string, isRemote bool) error
	ResolveAddressOrName(id string) (deployMsg *messages.DeployContract, registeredName string, info *ContractInfo, err error)
}

type ContractStore interface {
	ContractResolver
	BuildIndex()
	StoreNewContractInfo(addrHexNo0x, abiID, pathName, registerAs string) (*ContractInfo, error)
	AddFileToContractIndex(address, fileName string)
	AddToABIIndex(id string, deployMsg *messages.DeployContract, createdTime time.Time) *ABIInfo
	ListContracts() []messages.TimeSortable
	ListABIs() []messages.TimeSortable
}

type contractStore struct {
	baseURL               string
	storagePath           string
	rr                    RemoteRegistry
	contractIndex         map[string]messages.TimeSortable
	contractRegistrations map[string]*ContractInfo
	idxLock               sync.Mutex
	abiIndex              map[string]messages.TimeSortable
}

func NewContractStore(baseURL, storagePath string, rr RemoteRegistry) ContractStore {
	return &contractStore{
		baseURL:               baseURL,
		storagePath:           storagePath,
		rr:                    rr,
		contractIndex:         make(map[string]messages.TimeSortable),
		contractRegistrations: make(map[string]*ContractInfo),
		abiIndex:              make(map[string]messages.TimeSortable),
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

func (g *contractStore) StoreNewContractInfo(addrHexNo0x, abiID, pathName, registerAs string) (*ContractInfo, error) {
	contractInfo := &ContractInfo{
		Address:      addrHexNo0x,
		ABI:          abiID,
		Path:         "/contracts/" + pathName,
		SwaggerURL:   g.baseURL + "/contracts/" + pathName + "?swagger",
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

func IsRemote(msg messages.CommonHeaders) bool {
	ctxMap := msg.Context
	if isRemoteGeneric, ok := ctxMap[RemoteRegistryContextKey]; ok {
		if isRemote, ok := isRemoteGeneric.(bool); ok {
			return isRemote
		}
	}
	return false
}

func (g *contractStore) storeContractInfo(info *ContractInfo) error {
	if err := g.addToContractIndex(info); err != nil {
		return err
	}
	infoFile := path.Join(g.storagePath, "contract_"+info.Address+".instance.json")
	instanceBytes, _ := json.MarshalIndent(info, "", "  ")
	log.Infof("%s: Storing contract instance JSON to '%s'", info.ABI, infoFile)
	if err := ioutil.WriteFile(infoFile, instanceBytes, 0664); err != nil {
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreContractSave, err)
	}
	return nil
}

func (g *contractStore) ResolveContractAddr(registeredName string) (string, error) {
	nameUnescaped, _ := url.QueryUnescape(registeredName)
	info, exists := g.contractRegistrations[nameUnescaped]
	if !exists {
		return "", ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreContractLoad, registeredName)
	}
	log.Infof("%s -> 0x%s", registeredName, info.Address)
	return info.Address, nil
}

func (g *contractStore) LookupContractInstance(addrHex string) (*ContractInfo, error) {
	addrHexNo0x := strings.TrimPrefix(strings.ToLower(addrHex), "0x")
	info, exists := g.contractIndex[addrHexNo0x]
	if !exists {
		return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreContractNotFound, addrHexNo0x)
	}
	return info.(*ContractInfo), nil
}

func (g *contractStore) LoadDeployMsgByID(id string) (*messages.DeployContract, *ABIInfo, error) {
	var info *ABIInfo
	var msg *messages.DeployContract
	ts, exists := g.abiIndex[id]
	if !exists {
		log.Infof("ABI with ID %s not found locally", id)
		return nil, nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreABINotFound, id)
	}
	deployFile := path.Join(g.storagePath, "abi_"+id+".deploy.json")
	deployBytes, err := ioutil.ReadFile(deployFile)
	if err != nil {
		return nil, nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreABILoad, id, err)
	}
	msg = &messages.DeployContract{}
	if err = json.Unmarshal(deployBytes, msg); err != nil {
		return nil, nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreABIParse, id, err)
	}
	info = ts.(*ABIInfo)
	return msg, info, nil
}

func (g *contractStore) BuildIndex() {
	log.Infof("Building installed smart contract index")
	legacyContractMatcher, _ := regexp.Compile("^contract_([0-9a-z]{40})\\.swagger\\.json$")
	instanceMatcher, _ := regexp.Compile("^contract_([0-9a-z]{40})\\.instance\\.json$")
	abiMatcher, _ := regexp.Compile("^abi_([0-9a-z-]+)\\.deploy.json$")
	files, err := ioutil.ReadDir(g.storagePath)
	if err != nil {
		log.Errorf("Failed to read directory %s: %s", g.storagePath, err)
		return
	}
	for _, file := range files {
		fileName := file.Name()
		legacyContractGroups := legacyContractMatcher.FindStringSubmatch(fileName)
		abiGroups := abiMatcher.FindStringSubmatch(fileName)
		instanceGroups := instanceMatcher.FindStringSubmatch(fileName)
		if legacyContractGroups != nil {
			g.migrateLegacyContract(legacyContractGroups[1], path.Join(g.storagePath, fileName), file.ModTime())
		} else if instanceGroups != nil {
			g.AddFileToContractIndex(instanceGroups[1], path.Join(g.storagePath, fileName))
		} else if abiGroups != nil {
			g.addFileToABIIndex(abiGroups[1], path.Join(g.storagePath, fileName), file.ModTime())
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
		_, err := g.StoreNewContractInfo(address, ext.(string), address, registeredAs)
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

func (g *contractStore) AddFileToContractIndex(address, fileName string) {
	contractFile, err := os.OpenFile(fileName, os.O_RDONLY, 0)
	if err != nil {
		log.Errorf("Failed to load contract instance file %s: %s", fileName, err)
		return
	}
	defer contractFile.Close()
	var contractInfo ContractInfo
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
	g.AddToABIIndex(id, &deployMsg, createdTime)
}

func (g *contractStore) CheckNameAvailable(registerAs string, isRemote bool) error {
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

func (g *contractStore) addToContractIndex(info *ContractInfo) error {
	g.idxLock.Lock()
	defer g.idxLock.Unlock()
	if info.RegisteredAs != "" {
		// Protect against overwrite
		if err := g.CheckNameAvailable(info.RegisteredAs, false); err != nil {
			return err
		}
		log.Infof("Registering %s as '%s'", info.Address, info.RegisteredAs)
		g.contractRegistrations[info.RegisteredAs] = info
	}
	g.contractIndex[info.Address] = info
	return nil
}

func (g *contractStore) AddToABIIndex(id string, deployMsg *messages.DeployContract, createdTime time.Time) *ABIInfo {
	g.idxLock.Lock()
	info := &ABIInfo{
		ID:              id,
		Name:            deployMsg.ContractName,
		Description:     deployMsg.Description,
		Deployable:      len(deployMsg.Compiled) > 0,
		CompilerVersion: deployMsg.CompilerVersion,
		Path:            "/abis/" + id,
		SwaggerURL:      g.baseURL + "/abis/" + id + "?swagger",
		TimeSorted: messages.TimeSorted{
			CreatedISO8601: createdTime.UTC().Format(time.RFC3339),
		},
	}
	g.abiIndex[id] = info
	g.idxLock.Unlock()
	return info
}

func (g *contractStore) ListContracts() []messages.TimeSortable {
	g.idxLock.Lock()
	retval := make([]messages.TimeSortable, 0, len(g.contractIndex))
	for _, info := range g.contractIndex {
		retval = append(retval, info)
	}
	g.idxLock.Unlock()
	return retval
}

func (g *contractStore) ListABIs() []messages.TimeSortable {
	g.idxLock.Lock()
	retval := make([]messages.TimeSortable, 0, len(g.abiIndex))
	for _, info := range g.abiIndex {
		retval = append(retval, info)
	}
	g.idxLock.Unlock()
	return retval
}

func (g *contractStore) ResolveAddressOrName(id string) (deployMsg *messages.DeployContract, registeredName string, info *ContractInfo, err error) {
	info, err = g.LookupContractInstance(id)
	if err != nil {
		var origErr = err
		registeredName = id
		if id, err = g.ResolveContractAddr(registeredName); err != nil {
			log.Infof("%s is not a friendly name: %s", registeredName, err)
			return nil, "", nil, origErr
		}
		if info, err = g.LookupContractInstance(id); err != nil {
			return nil, "", nil, err
		}
	}
	deployMsg, _, err = g.LoadDeployMsgByID(info.ABI)
	return deployMsg, registeredName, info, err
}
