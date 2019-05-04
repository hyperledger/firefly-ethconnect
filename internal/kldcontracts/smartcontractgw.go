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
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-openapi/spec"
	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldopenapi"
	"github.com/kaleido-io/ethconnect/internal/kldtx"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	"github.com/spf13/cobra"

	"github.com/ethereum/go-ethereum/common/compiler"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/mholt/archiver"

	log "github.com/sirupsen/logrus"
)

const (
	maxFormParsingMemory = 32 << 20 // 32 MB
)

// SmartContractGateway provides gateway functions for OpenAPI 2.0 processing of Solidity contracts
type SmartContractGateway interface {
	PreDeploy(msg *kldmessages.DeployContract) error
	PostDeploy(msg *kldmessages.TransactionReceipt) error
	AddRoutes(router *httprouter.Router)
}

type smartContractGatewayInt interface {
	SmartContractGateway
	loadABIForInstance(addrHexNo0x string) (*kldmessages.ABI, error)
	loadDeployMsgForFactory(abi string) (*kldmessages.DeployContract, error)
}

// SmartContractGatewayConf configuration
type SmartContractGatewayConf struct {
	StoragePath string `json:"storagePath"`
	BaseURL     string `json:"baseURL"`
}

// CobraInitContractGateway standard naming for contract gateway command params
func CobraInitContractGateway(cmd *cobra.Command, conf *SmartContractGatewayConf) {
	cmd.Flags().StringVarP(&conf.StoragePath, "openapi-path", "I", "", "Path containing ABI + generated OpenAPI/Swagger 2.0 contact definitions")
	cmd.Flags().StringVarP(&conf.BaseURL, "openapi-baseurl", "U", "", "Base URL for generated OpenAPI/Swagger 2.0 contact definitions")
}

func (g *smartContractGW) AddRoutes(router *httprouter.Router) {
	g.r2e.addRoutes(router)
	router.GET("/contracts", g.listContractsOrABIs)
	router.GET("/contracts/:address", g.getContractOrABI)
	router.POST("/abis", g.addABI)
	router.GET("/abis", g.listContractsOrABIs)
	router.GET("/abis/:abi", g.getContractOrABI)
}

// NewSmartContractGateway construtor
func NewSmartContractGateway(conf *SmartContractGatewayConf, rpc kldeth.RPCClient, processor kldtx.TxnProcessor, asyncDispatcher REST2EthAsyncDispatcher) SmartContractGateway {
	var baseURL *url.URL
	var err error
	if conf.BaseURL != "" {
		if baseURL, err = url.Parse(conf.BaseURL); err != nil {
			log.Warnf("Unable to parse smart contract gateway base URL '%s': %s", conf.BaseURL, err)
		}
	}
	if baseURL == nil {
		baseURL, _ = url.Parse("http://localhost:8080")
	}
	log.Infof("OpenAPI Smart Contract Gateway configured with base URL '%s'", baseURL.String())
	abi2swagger := kldopenapi.NewABI2Swagger(baseURL.Host, baseURL.Path, []string{baseURL.Scheme})
	gw := &smartContractGW{
		conf:          conf,
		abi2swagger:   abi2swagger,
		contractIndex: make(map[string]timeSortable),
		abiIndex:      make(map[string]timeSortable),
	}
	syncDispatcher := newSyncDispatcher(processor)
	gw.r2e = newREST2eth(gw, rpc, asyncDispatcher, syncDispatcher)
	gw.buildIndex()
	return gw
}

type smartContractGW struct {
	conf          *SmartContractGatewayConf
	abi2swagger   *kldopenapi.ABI2Swagger
	r2e           *rest2eth
	contractIndex map[string]timeSortable
	idxLock       sync.Mutex
	abiIndex      map[string]timeSortable
}

type timeSortable interface {
	isLessThan(timeSortable, timeSortable) bool
	getISO8601() string
	getID() string
}

type timeSorted struct {
	CreatedISO8601 string `json:"created"`
}

// contractInfo is the minimal data structure we keep in memory, indexed by address
type contractInfo struct {
	timeSorted
	Address     string `json:"address"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	ABIURI      string `json:"abi"`
	SwaggerURL  string `json:"openapi"`
}

// abiInfo is the minimal data structure we keep in memory, indexed by our own UUID
type abiInfo struct {
	timeSorted
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Path            string `json:"path"`
	Deployable      bool   `json:"deployable"`
	SwaggerURL      string `json:"openapi"`
	CompilerVersion string `json:"compilerVersion"`
}

func (i *contractInfo) getID() string {
	return i.Address
}

func (i *abiInfo) getID() string {
	return i.ID
}

func (i *timeSorted) getISO8601() string {
	return i.CreatedISO8601
}

func (*timeSorted) isLessThan(i timeSortable, j timeSortable) bool {
	return i.getISO8601() > j.getISO8601() ||
		(i.getISO8601() == j.getISO8601() && i.getID() < j.getID())
}

// PostDeploy callback processes the transaction receipt and generates the Swagger
func (g *smartContractGW) PostDeploy(msg *kldmessages.TransactionReceipt) error {

	requestID := msg.Headers.ReqID
	requestFile := path.Join(g.conf.StoragePath, "abi_"+requestID+".deploy.json")
	var deployMsg kldmessages.DeployContract
	f, err := os.Open(requestFile)
	if err != nil {
		return fmt.Errorf("%s: Unable to recover pre-deploy message: %s", requestID, err)
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&deployMsg); err != nil {
		return fmt.Errorf("%s: Unable to read pre-deploy message: %s", requestID, err)
	}

	// We use the ethereum address of the contract, without the 0x prefix, and
	// all in lower case, as the name of the file and the path root of the Swagger operations
	addrHexNo0x := strings.ToLower(msg.ContractAddress.Hex()[2:])

	// Generate and store the swagger
	swagger, err := g.genSwagger(requestID, deployMsg.ContractName, deployMsg.ABI, deployMsg.DevDoc, addrHexNo0x)
	if err != nil {
		return err
	}
	g.addToContractIndex(addrHexNo0x, swagger, time.Now().UTC())

	urlBase := strings.ToLower(g.conf.BaseURL + "/contracts/" + strings.TrimPrefix(msg.ContractAddress.String(), "0x") + "?")
	msg.ContractSwagger = urlBase + "openapi"
	msg.ContractUI = urlBase + "ui"

	// Also store the corresponding ABI
	return g.storeABI(requestID, addrHexNo0x, deployMsg.ABI)
}

func (g *smartContractGW) genSwagger(requestID, apiName string, abi *kldmessages.ABI, devdoc string, addressOfInstance string) (*spec.Swagger, error) {

	if abi == nil {
		return nil, fmt.Errorf("ABI cannot be nil")
	}

	// Ensure we have a contract name in all cases, as the Swagger
	// won't be valid without a title
	if apiName == "" {
		apiName = requestID
	}
	var swagger *spec.Swagger
	var id, prefix string
	if addressOfInstance != "" {
		swagger = g.abi2swagger.Gen4Instance("/contracts/"+addressOfInstance, apiName, &abi.ABI, devdoc)
		id = addressOfInstance
		prefix = "contract"
	} else {
		swagger = g.abi2swagger.Gen4Factory("/abis/"+requestID, apiName, &abi.ABI, devdoc)
		id = requestID
		prefix = "abi"
	}

	// Add in an extension to the Swagger that points back at the filename of the deployment info
	swagger.Info.AddExtension("x-kaleido-deployment-id", requestID)

	swaggerFile := path.Join(g.conf.StoragePath, prefix+"_"+id+".swagger.json")
	swaggerBytes, _ := json.MarshalIndent(&swagger, "", "  ")
	log.Infof("%s: Storing OpenAPI JSON to '%s'", requestID, swaggerFile)
	if err := ioutil.WriteFile(swaggerFile, swaggerBytes, 0664); err != nil {
		return nil, fmt.Errorf("Failed to write OpenAPI JSON: %s", err)
	}
	return swagger, nil
}

func (g *smartContractGW) storeABI(requestID, addrHexNo0x string, abi *kldmessages.ABI) error {
	abiFile := path.Join(g.conf.StoragePath, "contract_"+addrHexNo0x+".abi.json")
	abiBytes, _ := json.MarshalIndent(abi, "", "  ")
	log.Infof("%s: Storing ABI JSON to '%s'", requestID, abiFile)
	if err := ioutil.WriteFile(abiFile, abiBytes, 0664); err != nil {
		return fmt.Errorf("Failed to write ABI JSON: %s", err)
	}
	return nil
}

func (g *smartContractGW) loadABIForInstance(addrHexNo0x string) (*kldmessages.ABI, error) {
	abiFile := path.Join(g.conf.StoragePath, "contract_"+addrHexNo0x+".abi.json")
	abiBytes, err := ioutil.ReadFile(abiFile)
	if err != nil {
		return nil, fmt.Errorf("Failed to find installed ABI for contract address 0x%s: %s", addrHexNo0x, err)
	}
	a := kldmessages.ABI{}
	if err = json.Unmarshal(abiBytes, &a); err != nil {
		return nil, fmt.Errorf("Failed to load installed ABI for contract address 0x%s: %s", addrHexNo0x, err)
	}
	return &a, nil
}

func (g *smartContractGW) loadDeployMsgForFactory(id string) (*kldmessages.DeployContract, error) {
	deployFile := path.Join(g.conf.StoragePath, "abi_"+id+".deploy.json")
	deployBytes, err := ioutil.ReadFile(deployFile)
	if err != nil {
		return nil, fmt.Errorf("Failed to find ABI with ID %s: %s", id, err)
	}
	msg := &kldmessages.DeployContract{}
	if err = json.Unmarshal(deployBytes, msg); err != nil {
		return nil, fmt.Errorf("Failed to load ABI with ID %s: %s", id, err)
	}
	return msg, nil
}

// PreDeploy
// - compiles the Solidity (if not precomplied),
// - puts the code into the message to avoid a recompile later
// - stores the ABI under the MsgID (can later be bound to an address)
// *** caller is responsible for ensuring unique Header.ID ***
func (g *smartContractGW) PreDeploy(msg *kldmessages.DeployContract) (err error) {
	solidity := msg.Solidity
	var compiled *kldeth.CompiledSolidity
	if solidity != "" {
		if compiled, err = kldeth.CompileContract(solidity, msg.ContractName, msg.CompilerVersion); err != nil {
			return err
		}
	}
	_, err = g.storeDeployableABI(msg, compiled)
	return err
}

func (g *smartContractGW) storeDeployableABI(msg *kldmessages.DeployContract, compiled *kldeth.CompiledSolidity) (*abiInfo, error) {

	if compiled != nil {
		msg.Compiled = compiled.Compiled
		msg.ABI = &kldmessages.ABI{
			ABI: *compiled.ABI,
		}
		msg.DevDoc = compiled.DevDoc
		msg.ContractName = compiled.ContractName
		msg.CompilerVersion = compiled.ContractInfo.CompilerVersion
	} else if msg.ABI == nil {
		return nil, fmt.Errorf("Must supply ABI to install an existing ABI into the REST Gateway")
	}

	requestID := msg.Headers.ID
	// We store the swagger in a generic format that can be used to deploy
	// additional instances, or generically call other instances
	// Generate and store the swagger
	swagger, err := g.genSwagger(requestID, msg.ContractName, msg.ABI, msg.DevDoc, "")
	if err != nil {
		return nil, err
	}
	msg.Description = swagger.Info.Description // Swagger generation parses the devdoc
	info := g.addToABIIndex(requestID, msg, time.Now().UTC())

	g.writeAbiInfo(requestID, msg)

	// We remove the solidity payload from the message, as we've consumed
	// it by compiling and there is no need to serialize it again.
	// The messages should contain compiled bytes at this
	msg.Solidity = ""

	return info, nil

}

func (g *smartContractGW) gatewayErrReply(res http.ResponseWriter, req *http.Request, err error, status int) {
	log.Errorf("<-- %s %s [%d]: %s", req.Method, req.URL, status, err)
	reply, _ := json.Marshal(&restErrMsg{Message: err.Error()})
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	res.Write(reply)
	return
}

func (g *smartContractGW) writeAbiInfo(requestID string, msg *kldmessages.DeployContract) error {
	// We store all the details from our compile, or the user-supplied
	// details, in a file under the message ID.
	infoFile := path.Join(g.conf.StoragePath, "abi_"+requestID+".deploy.json")
	infoBytes, _ := json.MarshalIndent(msg, "", "  ")
	log.Infof("%s: Stashing deployment details to '%s'", requestID, infoFile)
	if err := ioutil.WriteFile(infoFile, infoBytes, 0664); err != nil {
		return fmt.Errorf("%s: Failed to write deployment details: %s", requestID, err)
	}
	return nil
}

func (g *smartContractGW) buildIndex() {
	log.Infof("Building installed smart contract index")
	contractMatcher, _ := regexp.Compile("^contract_([0-9a-z]{40})\\.swagger\\.json$")
	abiMatcher, _ := regexp.Compile("^abi_([0-9a-z-]+)\\.deploy.json$")
	files, err := ioutil.ReadDir(g.conf.StoragePath)
	if err != nil {
		log.Errorf("Failed to read directory %s: %s", g.conf.StoragePath, err)
		return
	}
	for _, file := range files {
		fileName := file.Name()
		contractGroups := contractMatcher.FindStringSubmatch(fileName)
		abiGroups := abiMatcher.FindStringSubmatch(fileName)
		if contractGroups != nil {
			g.addFileToContractIndex(contractGroups[1], path.Join(g.conf.StoragePath, fileName), file.ModTime())
		} else if abiGroups != nil {
			g.addFileToABIIndex(abiGroups[1], path.Join(g.conf.StoragePath, fileName), file.ModTime())
		}
	}
	log.Infof("Smart contract index built. %d entries", len(g.contractIndex))
}

func (g *smartContractGW) addFileToContractIndex(address, fileName string, createdTime time.Time) {
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
	if swagger.Info != nil {
		g.addToContractIndex(address, &swagger, createdTime)
	}
}

func (g *smartContractGW) addFileToABIIndex(id, fileName string, createdTime time.Time) {
	deployFile, err := os.OpenFile(fileName, os.O_RDONLY, 0)
	if err != nil {
		log.Errorf("Failed to load ABI deployment file %s: %s", fileName, err)
		return
	}
	defer deployFile.Close()
	var deployMsg kldmessages.DeployContract
	err = json.NewDecoder(bufio.NewReader(deployFile)).Decode(&deployMsg)
	if err != nil {
		log.Errorf("Failed to parse ABI deployment file %s: %s", fileName, err)
		return
	}
	g.addToABIIndex(id, &deployMsg, createdTime)
}

func (g *smartContractGW) addToContractIndex(address string, swagger *spec.Swagger, createdTime time.Time) {
	g.idxLock.Lock()
	var abiURI string
	if ext, exists := swagger.Info.Extensions["x-kaleido-deployment-id"]; exists {
		abiURI = "/abis/" + ext.(string)
	}
	g.contractIndex[address] = &contractInfo{
		Address:     address,
		Name:        swagger.Info.Title,
		Description: swagger.Info.Description,
		ABIURI:      abiURI,
		Path:        "/contracts/" + address,
		SwaggerURL:  g.conf.BaseURL + "/contracts/" + address + "?swagger",
		timeSorted: timeSorted{
			CreatedISO8601: createdTime.UTC().Format(time.RFC3339),
		},
	}
	g.idxLock.Unlock()
}

func (g *smartContractGW) addToABIIndex(id string, deployMsg *kldmessages.DeployContract, createdTime time.Time) *abiInfo {
	g.idxLock.Lock()
	info := &abiInfo{
		ID:              id,
		Name:            deployMsg.ContractName,
		Description:     deployMsg.Description,
		Deployable:      len(deployMsg.Compiled) > 0,
		CompilerVersion: deployMsg.CompilerVersion,
		Path:            "/abis/" + id,
		SwaggerURL:      g.conf.BaseURL + "/abis/" + id + "?swagger",
		timeSorted: timeSorted{
			CreatedISO8601: createdTime.UTC().Format(time.RFC3339),
		},
	}
	g.abiIndex[id] = info
	g.idxLock.Unlock()
	return info
}

// listContracts sorts by Title then Address and returns an array
func (g *smartContractGW) listContractsOrABIs(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	var index map[string]timeSortable
	if strings.HasSuffix(req.URL.Path, "contracts") {
		index = g.contractIndex
	} else {
		index = g.abiIndex
	}

	// Get an array copy of the current list
	g.idxLock.Lock()
	retval := make([]timeSortable, 0, len(index))
	for _, info := range index {
		retval = append(retval, info)
	}
	g.idxLock.Unlock()

	// Do the sort by Title then Address
	sort.Slice(retval, func(i, j int) bool {
		return retval[i].isLessThan(retval[i], retval[j])
	})

	status := 200
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	enc := json.NewEncoder(res)
	enc.SetIndent("", "  ")
	enc.Encode(&retval)
}

func (g *smartContractGW) getContractOrABI(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	req.ParseForm()
	swaggerRequest := false
	uiRequest := false
	if vs := req.Form["swagger"]; len(vs) > 0 {
		swaggerRequest = true
	}
	if vs := req.Form["openapi"]; len(vs) > 0 {
		swaggerRequest = true
	}
	if vs := req.Form["ui"]; len(vs) > 0 {
		uiRequest = true
	}
	id := strings.TrimPrefix(strings.ToLower(params.ByName("address")), "0x")
	prefix := "contract"
	var index map[string]timeSortable
	index = g.contractIndex
	if id == "" {
		id = strings.ToLower(params.ByName("abi"))
		prefix = "abi"
		index = g.abiIndex
	}
	// For safety we always check our sanitized address index in memory, before checking the filesystem
	from := req.FormValue("from")
	if info, exists := index[id]; exists {
		if uiRequest {
			fromQuery := ""
			if from != "" {
				fromQuery = "&from=" + url.QueryEscape(from)
			}
			g.writeHTMLForUI(prefix, id, fromQuery, res)
		} else if swaggerRequest {
			swaggerPath := path.Join(g.conf.StoragePath, prefix+"_"+id+".swagger.json")
			log.Infof("Returning %s", swaggerPath)
			swaggerBytes, err := ioutil.ReadFile(swaggerPath)
			if err != nil {
				g.gatewayErrReply(res, req, fmt.Errorf("Failed to read OpenAPI definition"), 500)
				return
			}
			if from != "" {
				var swagger spec.Swagger
				err = json.Unmarshal(swaggerBytes, &swagger)
				if err != nil {
					g.gatewayErrReply(res, req, fmt.Errorf("Failed to parse stored OpenAPI definition"), 500)
					return
				}
				if swagger.Parameters != nil {
					if param, exists := swagger.Parameters["fromParam"]; exists {
						param.SimpleSchema.Default = from
						swagger.Parameters["fromParam"] = param
					}
				}
				swaggerBytes, _ = json.Marshal(&swagger)
			}
			log.Infof("<-- %s %s [%d]", req.Method, req.URL, 200)
			res.Header().Set("Content-Type", "application/json")
			if vs := req.Form["download"]; len(vs) > 0 {
				res.Header().Set("Content-Disposition", "attachment; filename=\""+id+".swagger.json\"")
			}
			res.WriteHeader(200)
			res.Write(swaggerBytes)
		} else {
			log.Infof("<-- %s %s [%d]", req.Method, req.URL, 200)
			res.Header().Set("Content-Type", "application/json")
			res.WriteHeader(200)
			enc := json.NewEncoder(res)
			enc.SetIndent("", "  ")
			enc.Encode(info)
		}
	} else {
		g.gatewayErrReply(res, req, fmt.Errorf("Not found"), 404)
	}
}

func tempdir() string {
	dir, _ := ioutil.TempDir("", "kld")
	log.Infof("tmpdir/create: %s", dir)
	return dir
}

func cleanup(dir string) {
	log.Infof("tmpdir/cleanup: %s [dir]", dir)
	os.RemoveAll(dir)
}

func (g *smartContractGW) addABI(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	if err := req.ParseMultipartForm(maxFormParsingMemory); err != nil {
		g.gatewayErrReply(res, req, fmt.Errorf("Could not parse supplied multi-part form data: %s", err), 400)
		return
	}

	tempdir := tempdir()
	defer cleanup(tempdir)
	for name, files := range req.MultipartForm.File {
		log.Debugf("multi-part form entry '%s'", name)
		for _, fileHeader := range files {
			if err := g.extractMultiPartFile(tempdir, fileHeader); err != nil {
				g.gatewayErrReply(res, req, err, 400)
				return
			}
		}
	}

	compiled, err := g.compileMultipartFormSolidity(tempdir, req)
	if err != nil {
		g.gatewayErrReply(res, req, fmt.Errorf("Failed to compile solidity: %s", err), 400)
		return
	}

	msg := &kldmessages.DeployContract{}
	msg.Headers.MsgType = kldmessages.MsgTypeSendTransaction
	msg.Headers.ID = kldutils.UUIDv4()
	info, err := g.storeDeployableABI(msg, compiled)
	if err != nil {
		g.gatewayErrReply(res, req, err, 500)
		return
	}

	log.Infof("<-- %s %s [%d]", req.Method, req.URL, 200)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(200)
	json.NewEncoder(res).Encode(info)
}

func (g *smartContractGW) compileMultipartFormSolidity(dir string, req *http.Request) (*kldeth.CompiledSolidity, error) {
	solFiles := []string{}
	rootFiles, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Errorf("Failed to read dir '%s': %s", dir, err)
		return nil, fmt.Errorf("Failed to read extracted multi-part form data")
	}
	for _, file := range rootFiles {
		log.Debugf("multi-part: '%s' [dir=%t]", file.Name(), file.IsDir())
		if strings.HasSuffix(file.Name(), ".sol") {
			solFiles = append(solFiles, file.Name())
		}
	}

	solcArgs := []string{
		"--combined-json", "bin,bin-runtime,srcmap,srcmap-runtime,abi,userdoc,devdoc,metadata",
		"--optimize",
		"--allow-paths", ".",
	}
	if sourceFiles := req.Form["source"]; len(sourceFiles) > 0 {
		solcArgs = append(solcArgs, sourceFiles...)
	} else if len(solFiles) > 0 {
		solcArgs = append(solcArgs, solFiles...)
	} else {
		return nil, fmt.Errorf("No .sol files found in root. Please set a 'source' query param or form field to the relative path of your solidity")
	}

	solcExec, err := kldeth.GetSolc(req.FormValue("compiler"))
	if err != nil {
		return nil, err
	}
	solcVer, err := compiler.SolidityVersion(solcExec)
	if err != nil {
		log.Errorf("Failed to find solc: %s", err)
		return nil, fmt.Errorf("Failed checking solc version")
	}
	solOptionsString := strings.Join(append([]string{solcVer.Path}, solcArgs...), " ")
	log.Infof("Compiling: %s", solOptionsString)
	cmd := exec.Command(solcVer.Path, solcArgs...)

	var stderr, stdout bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("Failed to compile [%s]: %s", err, stderr.String())
	}

	compiled, err := compiler.ParseCombinedJSON(stdout.Bytes(), "", solcVer.Version, solcVer.Version, solOptionsString)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse solc output: %s", err)
	}

	return kldeth.ProcessCompiled(compiled, req.FormValue("contract"), false)
}

func (g *smartContractGW) extractMultiPartFile(dir string, file *multipart.FileHeader) error {
	fileName := file.Filename
	if strings.ContainsAny(fileName, "/\\") {
		return fmt.Errorf("Filenames cannot contain slashes. Use a zip file to upload a directory structure")
	}
	in, err := file.Open()
	if err != nil {
		log.Errorf("Failed opening '%s' for reading: %s", fileName, err)
		return fmt.Errorf("Failed to read archive")
	}
	defer in.Close()
	outFileName := path.Join(dir, fileName)
	out, err := os.OpenFile(outFileName, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Errorf("Failed opening '%s' for writing: %s", fileName, err)
		return fmt.Errorf("Failed to process archive")
	}
	written, err := io.Copy(out, in)
	if err != nil {
		log.Errorf("Failed writing '%s' from multi-part form: %s", fileName, err)
		return fmt.Errorf("Failed to process archive")
	}
	log.Debugf("multi-part: '%s' [%dKb]", fileName, written/1024)
	return g.processIfArchive(dir, outFileName)
}

func (g *smartContractGW) processIfArchive(dir, fileName string) error {
	z, err := archiver.ByExtension(fileName)
	if err != nil {
		log.Debugf("multi-part: '%s' not an archive: %s", fileName, err)
		return nil
	}
	err = z.(archiver.Unarchiver).Unarchive(fileName, dir)
	if err != nil {
		return fmt.Errorf("Error unarchiving supplied zip file: %s", err)
	}
	return nil
}

// Write out a nice little UI for exercising the Swagger
func (g *smartContractGW) writeHTMLForUI(prefix, id, fromQuery string, res http.ResponseWriter) {
	html := `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">
<html>
<head>
  <meta charset="utf-8"> <!-- Important: rapi-doc uses utf8 charecters -->
  <script src="https://unpkg.com/rapidoc/dist/rapidoc-min.js"></script>
</head>
<body>
  <rapi-doc 
    spec-url="` + g.conf.BaseURL + "/" + prefix + "s/" + id + "?swagger" + fromQuery + `"
    allow-authentication="false"
    allow-spec-url-load="false"
    allow-spec-file-load="false"
    heading-text="Ethconnect REST Gateway"
    header-color="#3842C1"
    theme="light"
    primary-color="#3842C1"
  >
    <img 
      slot="logo" 
      src="//bit.ly/kaleido-logo-header-small-purple"
      alt="Kaleido"
      onclick="window.open('https://kaleido.io')"
      style="cursor: pointer;"
    />
  </rapi-doc>
</body> 
</html>
`
	res.Header().Set("Content-Type", "text/html; charset=utf-8")
	res.WriteHeader(200)
	res.Write([]byte(html))
}
