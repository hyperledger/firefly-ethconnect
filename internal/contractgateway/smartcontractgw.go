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

package contractgateway

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-openapi/spec"
	"github.com/julienschmidt/httprouter"
	"github.com/mholt/archiver"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/hyperledger/firefly-ethconnect/internal/auth"
	"github.com/hyperledger/firefly-ethconnect/internal/contractregistry"
	ethconnecterrors "github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/eth"
	"github.com/hyperledger/firefly-ethconnect/internal/ethbind"
	"github.com/hyperledger/firefly-ethconnect/internal/events"
	"github.com/hyperledger/firefly-ethconnect/internal/messages"
	"github.com/hyperledger/firefly-ethconnect/internal/openapi"
	"github.com/hyperledger/firefly-ethconnect/internal/tx"
	"github.com/hyperledger/firefly-ethconnect/internal/utils"
	"github.com/hyperledger/firefly-ethconnect/internal/ws"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
)

const (
	maxFormParsingMemory   = 32 << 20 // 32 MB
	errEventSupportMissing = "Event support is not configured on this gateway"
)

// remoteContractInfo is the ABI raw data back out of the REST API gateway with bytecode
type remoteContractInfo struct {
	ID      string                   `json:"id"`
	Address string                   `json:"address,omitempty"`
	ABI     ethbinding.ABIMarshaling `json:"abi"`
}

// SmartContractGateway provides gateway functions for OpenAPI 2.0 processing of Solidity contracts
type SmartContractGateway interface {
	PreDeploy(msg *messages.DeployContract) error
	PostDeploy(msg *messages.TransactionReceipt) error
	AddRoutes(router *httprouter.Router)
	SendReply(message interface{})
	Shutdown()
}

// SmartContractGatewayConf configuration
type SmartContractGatewayConf struct {
	events.SubscriptionManagerConf
	StoragePath    string                              `json:"storagePath"`
	BaseURL        string                              `json:"baseURL"`
	RemoteRegistry contractregistry.RemoteRegistryConf `json:"registry,omitempty"` // JSON only config - no commandline
}

// CobraInitContractGateway standard naming for contract gateway command params
func CobraInitContractGateway(cmd *cobra.Command, conf *SmartContractGatewayConf) {
	cmd.Flags().StringVarP(&conf.StoragePath, "openapi-path", "I", "", "Path containing ABI + generated OpenAPI/Swagger 2.0 contact definitions")
	cmd.Flags().StringVarP(&conf.BaseURL, "openapi-baseurl", "U", "", "Base URL for generated OpenAPI/Swagger 2.0 contact definitions")
	events.CobraInitSubscriptionManager(cmd, &conf.SubscriptionManagerConf)
}

func (g *smartContractGW) withEventsAuth(handler httprouter.Handle) httprouter.Handle {
	return func(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
		err := auth.AuthEventStreams(req.Context())
		if err != nil {
			log.Errorf("Unauthorized: %s", err)
			g.gatewayErrReply(res, req, ethconnecterrors.Errorf(ethconnecterrors.Unauthorized), 401)
			return
		}
		handler(res, req, params)
	}
}

func (g *smartContractGW) AddRoutes(router *httprouter.Router) {
	g.r2e.addRoutes(router)
	router.GET("/contracts", g.listContractsOrABIs)
	router.GET("/contracts/:address", g.getContractOrABI)
	router.POST("/abis", g.addABI)
	router.GET("/abis", g.listContractsOrABIs)
	router.GET("/abis/:abi", g.getContractOrABI)
	router.POST("/abis/:abi/:address", g.registerContract)
	router.GET("/instances/:instance_lookup", g.getRemoteRegistrySwaggerOrABI)
	router.GET("/i/:instance_lookup", g.getRemoteRegistrySwaggerOrABI)
	router.GET("/gateways/:gateway_lookup", g.getRemoteRegistrySwaggerOrABI)
	router.GET("/g/:gateway_lookup", g.getRemoteRegistrySwaggerOrABI)
	router.POST(events.StreamPathPrefix, g.withEventsAuth(g.createStream))
	router.PATCH(events.StreamPathPrefix+"/:id", g.withEventsAuth(g.updateStream))
	router.GET(events.StreamPathPrefix, g.withEventsAuth(g.listStreamsOrSubs))
	router.POST(events.SubPathPrefix, g.withEventsAuth(g.addSub))
	router.GET(events.SubPathPrefix, g.withEventsAuth(g.listStreamsOrSubs))
	router.GET(events.StreamPathPrefix+"/:id", g.withEventsAuth(g.getStreamOrSub))
	router.GET(events.SubPathPrefix+"/:id", g.withEventsAuth(g.getStreamOrSub))
	router.DELETE(events.StreamPathPrefix+"/:id", g.withEventsAuth(g.deleteStreamOrSub))
	router.DELETE(events.SubPathPrefix+"/:id", g.withEventsAuth(g.deleteStreamOrSub))
	router.POST(events.SubPathPrefix+"/:id/reset", g.withEventsAuth(g.resetSub))
	router.POST(events.StreamPathPrefix+"/:id/suspend", g.withEventsAuth(g.suspendOrResumeStream))
	router.POST(events.StreamPathPrefix+"/:id/resume", g.withEventsAuth(g.suspendOrResumeStream))
}

func (g *smartContractGW) SendReply(message interface{}) {
	g.ws.SendReply(message)
}

// NewSmartContractGateway constructor
func NewSmartContractGateway(conf *SmartContractGatewayConf, txnConf *tx.TxnProcessorConf, rpc eth.RPCClient, processor tx.TxnProcessor, asyncDispatcher REST2EthAsyncDispatcher, ws ws.WebSocketChannels) (SmartContractGateway, error) {
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
	gw := &smartContractGW{
		conf: conf,
		baseSwaggerConf: &openapi.ABI2SwaggerConf{
			ExternalHost:     baseURL.Host,
			ExternalRootPath: baseURL.Path,
			ExternalSchemes:  []string{baseURL.Scheme},
			OrionPrivateAPI:  txnConf.OrionPrivateAPIS,
			BasicAuth:        true,
		},
		ws: ws,
	}
	rr := contractregistry.NewRemoteRegistry(&conf.RemoteRegistry)
	gw.cs = contractregistry.NewContractStore(&contractregistry.ContractStoreConf{
		BaseURL:     conf.BaseURL,
		StoragePath: conf.StoragePath,
	}, rr)
	if err = gw.cs.Init(); err != nil {
		return nil, err
	}
	syncDispatcher := newSyncDispatcher(processor)
	if conf.EventLevelDBPath != "" {
		gw.sm = events.NewSubscriptionManager(&conf.SubscriptionManagerConf, rpc, gw.cs, gw.ws)
		err = gw.sm.Init()
		if err != nil {
			return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayEventManagerInitFailed, err)
		}
	}
	gw.r2e = newREST2eth(gw, gw.cs, rpc, gw.sm, processor, asyncDispatcher, syncDispatcher)
	return gw, nil
}

type smartContractGW struct {
	conf            *SmartContractGatewayConf
	sm              events.SubscriptionManager
	cs              contractregistry.ContractStore
	r2e             *rest2eth
	ws              ws.WebSocketChannels
	baseSwaggerConf *openapi.ABI2SwaggerConf
}

// PostDeploy callback processes the transaction receipt and generates the Swagger
func (g *smartContractGW) PostDeploy(msg *messages.TransactionReceipt) error {

	requestID := msg.Headers.ReqID

	// We use the ethereum address of the contract, without the 0x prefix, and
	// all in lower case, as the name of the file and the path root of the Swagger operations
	if msg.ContractAddress == nil {
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayPostDeployMissingAddress, requestID)
	}
	addrHexNo0x := strings.ToLower(msg.ContractAddress.Hex()[2:])

	// Generate and store the swagger
	basePath := "/contracts/"
	isRemote := contractregistry.IsRemote(msg.Headers.CommonHeaders)
	if isRemote {
		basePath = "/instances/"
	}
	registeredName := msg.RegisterAs
	if registeredName == "" {
		registeredName = addrHexNo0x
	}

	if msg.Headers.MsgType == messages.MsgTypeTransactionSuccess {
		msg.ContractSwagger = g.conf.BaseURL + basePath + registeredName + "?openapi"
		msg.ContractUI = g.conf.BaseURL + basePath + registeredName + "?ui"

		var err error
		if isRemote {
			if msg.RegisterAs != "" {
				err = g.cs.AddRemoteInstance(msg.RegisterAs, "0x"+addrHexNo0x)
			}
		} else {
			abiID := requestID
			if msg.Headers.ReqABIID != "" {
				// This was invoked against an existing ABI, so we need to add an instance there
				abiID = msg.Headers.ReqABIID
			}
			_, err = g.cs.AddContract(addrHexNo0x, abiID, registeredName, msg.RegisterAs)
		}
		return err
	}
	return nil
}

func (g *smartContractGW) swaggerForRemoteRegistry(swaggerGen *openapi.ABI2Swagger, apiName, addr string, factoryOnly bool, abi *ethbinding.RuntimeABI, devdoc, path string) *spec.Swagger {
	var swagger *spec.Swagger
	if addr == "" {
		swagger = swaggerGen.Gen4Factory(path, apiName, factoryOnly, true, &abi.ABI, devdoc)
	} else {
		swagger = swaggerGen.Gen4Instance(path, apiName, &abi.ABI, devdoc)
	}
	return swagger
}

func (g *smartContractGW) swaggerForABI(swaggerGen *openapi.ABI2Swagger, abiID, apiName string, factoryOnly bool, abi *ethbinding.RuntimeABI, devdoc string, addrHexNo0x, registerAs string) *spec.Swagger {
	// Ensure we have a contract name in all cases, as the Swagger
	// won't be valid without a title
	if apiName == "" {
		apiName = abiID
	}
	var swagger *spec.Swagger
	if addrHexNo0x != "" {
		pathSuffix := url.QueryEscape(registerAs)
		if pathSuffix == "" {
			pathSuffix = addrHexNo0x
		}
		swagger = swaggerGen.Gen4Instance("/contracts/"+pathSuffix, apiName, &abi.ABI, devdoc)
		if registerAs != "" {
			swagger.Info.AddExtension("x-firefly-registered-name", pathSuffix)
		}
	} else {
		swagger = swaggerGen.Gen4Factory("/abis/"+abiID, apiName, factoryOnly, false, &abi.ABI, devdoc)
	}

	// Add in an extension to the Swagger that points back at the filename of the deployment info
	if abiID != "" {
		swagger.Info.AddExtension("x-firefly-deployment-id", abiID)
	}

	return swagger
}

// PreDeploy
// - compiles the Solidity (if not precomplied),
// - puts the code into the message to avoid a recompile later
// - stores the ABI under the MsgID (can later be bound to an address)
// *** caller is responsible for ensuring unique Header.ID ***
func (g *smartContractGW) PreDeploy(msg *messages.DeployContract) (err error) {
	solidity := msg.Solidity
	var compiled *eth.CompiledSolidity
	if solidity != "" {
		if compiled, err = eth.CompileContract(solidity, msg.ContractName, msg.CompilerVersion, msg.EVMVersion); err != nil {
			return err
		}
	}
	if !contractregistry.IsRemote(msg.Headers.CommonHeaders) {
		_, err = g.storeDeployableABI(msg, compiled)
	}
	return err
}

func (g *smartContractGW) storeDeployableABI(msg *messages.DeployContract, compiled *eth.CompiledSolidity) (*contractregistry.ABIInfo, error) {

	if compiled != nil {
		msg.Compiled = compiled.Compiled
		msg.ABI = compiled.ABI
		msg.DevDoc = compiled.DevDoc
		msg.ContractName = compiled.ContractName
		msg.CompilerVersion = compiled.ContractInfo.CompilerVersion
	} else if msg.ABI == nil {
		return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreMissingABI)
	}

	runtimeABI, err := ethbind.API.ABIMarshalingToABIRuntime(msg.ABI)
	if err != nil {
		return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayInvalidABI, err)
	}

	requestID := msg.Headers.ID
	// We store the swagger in a generic format that can be used to deploy
	// additional instances, or generically call other instances
	// Generate and store the swagger
	swagger := g.swaggerForABI(openapi.NewABI2Swagger(g.baseSwaggerConf), requestID, msg.ContractName, false, runtimeABI, msg.DevDoc, "", "")
	msg.Description = swagger.Info.Description // Swagger generation parses the devdoc
	info := g.cs.AddABI(requestID, msg, time.Now().UTC())

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

func (g *smartContractGW) writeAbiInfo(requestID string, msg *messages.DeployContract) error {
	// We store all the details from our compile, or the user-supplied
	// details, in a file under the message ID.
	infoFile := path.Join(g.conf.StoragePath, "abi_"+requestID+".deploy.json")
	infoBytes, _ := json.MarshalIndent(msg, "", "  ")
	log.Infof("%s: Stashing deployment details to '%s'", requestID, infoFile)
	if err := ioutil.WriteFile(infoFile, infoBytes, 0664); err != nil {
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayLocalStoreContractSavePostDeploy, requestID, err)
	}
	return nil
}

// listContractsOrABIs sorts by Title then Address and returns an array
func (g *smartContractGW) listContractsOrABIs(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	var retval []messages.TimeSortable
	if strings.HasSuffix(req.URL.Path, "contracts") {
		retval = g.cs.ListContracts()
	} else {
		retval = g.cs.ListABIs()
	}

	status := 200
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	enc := json.NewEncoder(res)
	enc.SetIndent("", "  ")
	enc.Encode(&retval)
}

// createStream creates a stream
func (g *smartContractGW) createStream(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	if g.sm == nil {
		g.gatewayErrReply(res, req, errors.New(errEventSupportMissing), 405)
		return
	}

	var spec events.StreamInfo
	if err := json.NewDecoder(req.Body).Decode(&spec); err != nil {
		g.gatewayErrReply(res, req, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayEventStreamInvalid, err), 400)
		return
	}

	newSpec, err := g.sm.AddStream(req.Context(), &spec)
	if err != nil {
		g.gatewayErrReply(res, req, err, 400)
		return
	}

	status := 200
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	enc := json.NewEncoder(res)
	enc.SetIndent("", "  ")
	enc.Encode(&newSpec)
}

// updateStream updates a stream
func (g *smartContractGW) updateStream(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	if g.sm == nil {
		g.gatewayErrReply(res, req, errors.New(errEventSupportMissing), 405)
		return
	}

	streamID := params.ByName("id")
	_, err := g.sm.StreamByID(req.Context(), streamID)
	if err != nil {
		g.gatewayErrReply(res, req, err, 404)
		return
	}
	var spec events.StreamInfo
	if err := json.NewDecoder(req.Body).Decode(&spec); err != nil {
		g.gatewayErrReply(res, req, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayEventStreamInvalid, err), 400)
		return
	}
	newSpec, err := g.sm.UpdateStream(req.Context(), streamID, &spec)
	if err != nil {
		g.gatewayErrReply(res, req, err, 500)
		return
	}

	status := 200
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	enc := json.NewEncoder(res)
	enc.SetIndent("", "  ")
	enc.Encode(&newSpec)
}

// listStreamsOrSubs sorts by Title then Address and returns an array
func (g *smartContractGW) listStreamsOrSubs(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	if g.sm == nil {
		g.gatewayErrReply(res, req, errors.New(errEventSupportMissing), 405)
		return
	}

	var results []messages.TimeSortable
	if strings.HasPrefix(req.URL.Path, events.SubPathPrefix) {
		subs := g.sm.Subscriptions(req.Context())
		results = make([]messages.TimeSortable, len(subs))
		for i := range subs {
			results[i] = subs[i]
		}
	} else {
		streams := g.sm.Streams(req.Context())
		results = make([]messages.TimeSortable, len(streams))
		for i := range streams {
			results[i] = streams[i]
		}
	}

	// Do the sort
	sort.Slice(results, func(i, j int) bool {
		return results[i].IsLessThan(results[i], results[j])
	})

	status := 200
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	enc := json.NewEncoder(res)
	enc.SetIndent("", "  ")
	enc.Encode(&results)
}

// getStreamOrSub returns stream over REST
func (g *smartContractGW) getStreamOrSub(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	if g.sm == nil {
		g.gatewayErrReply(res, req, errors.New(errEventSupportMissing), 405)
		return
	}

	var retval interface{}
	var err error
	if strings.HasPrefix(req.URL.Path, events.SubPathPrefix) {
		retval, err = g.sm.SubscriptionByID(req.Context(), params.ByName("id"))
	} else {
		retval, err = g.sm.StreamByID(req.Context(), params.ByName("id"))
	}
	if err != nil {
		g.gatewayErrReply(res, req, err, 404)
		return
	}

	status := 200
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	enc := json.NewEncoder(res)
	enc.SetIndent("", "  ")
	enc.Encode(retval)
}

// deleteStreamOrSub deletes stream over REST
func (g *smartContractGW) deleteStreamOrSub(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	if g.sm == nil {
		g.gatewayErrReply(res, req, errors.New(errEventSupportMissing), 405)
		return
	}

	var err error
	if strings.HasPrefix(req.URL.Path, events.SubPathPrefix) {
		err = g.sm.DeleteSubscription(req.Context(), params.ByName("id"))
	} else {
		err = g.sm.DeleteStream(req.Context(), params.ByName("id"))
	}
	if err != nil {
		g.gatewayErrReply(res, req, err, 500)
		return
	}

	status := 204
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
}

// addSub resets subscription over REST
func (g *smartContractGW) addSub(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	if g.sm == nil {
		g.gatewayErrReply(res, req, errors.New(errEventSupportMissing), 405)
		return
	}

	var retval interface{}
	var body events.SubscriptionCreateDTO
	err := json.NewDecoder(req.Body).Decode(&body)
	if err == nil {
		retval, err = g.sm.AddSubscriptionDirect(req.Context(), &body)
	}
	if err != nil {
		g.gatewayErrReply(res, req, err, 500)
		return
	}

	status := 201
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	enc := json.NewEncoder(res)
	enc.SetIndent("", "  ")
	enc.Encode(retval)
}

// resetSub resets subscription over REST
func (g *smartContractGW) resetSub(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	if g.sm == nil {
		g.gatewayErrReply(res, req, errors.New(errEventSupportMissing), 405)
		return
	}

	var body struct {
		FromBlock string `json:"fromBlock"`
	}
	err := json.NewDecoder(req.Body).Decode(&body)
	if err == nil {
		err = g.sm.ResetSubscription(req.Context(), params.ByName("id"), body.FromBlock)
	}
	if err != nil {
		g.gatewayErrReply(res, req, err, 500)
		return
	}

	status := 204
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
}

// suspendOrResumeStream suspends or resumes a stream
func (g *smartContractGW) suspendOrResumeStream(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	if g.sm == nil {
		g.gatewayErrReply(res, req, errors.New(errEventSupportMissing), 405)
		return
	}

	var err error
	if strings.HasSuffix(req.URL.Path, "resume") {
		err = g.sm.ResumeStream(req.Context(), params.ByName("id"))
	} else {
		err = g.sm.SuspendStream(req.Context(), params.ByName("id"))
	}
	if err != nil {
		g.gatewayErrReply(res, req, err, 500)
		return
	}

	status := 204
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
}

func (g *smartContractGW) isSwaggerRequest(req *http.Request) (swaggerGen *openapi.ABI2Swagger, uiRequest, factoryOnly, abiRequest, refreshABI bool, from string) {
	req.ParseForm()
	var swaggerRequest bool
	if vs := req.Form["swagger"]; len(vs) > 0 {
		swaggerRequest = strings.ToLower(vs[0]) != "false"
	}
	if vs := req.Form["openapi"]; len(vs) > 0 {
		swaggerRequest = strings.ToLower(vs[0]) != "false"
	}
	if vs := req.Form["ui"]; len(vs) > 0 {
		uiRequest = strings.ToLower(vs[0]) != "false"
	}
	if vs := req.Form["factory"]; len(vs) > 0 {
		factoryOnly = strings.ToLower(vs[0]) != "false"
	}
	if vs := req.Form["abi"]; len(vs) > 0 {
		abiRequest = strings.ToLower(vs[0]) != "false"
	}
	if vs := req.Form["refresh"]; len(vs) > 0 {
		refreshABI = strings.ToLower(vs[0]) != "false"
	}
	from = req.FormValue("from")
	if swaggerRequest {
		var conf = *g.baseSwaggerConf
		if vs := req.Form["noauth"]; len(vs) > 0 {
			conf.BasicAuth = strings.ToLower(vs[0]) == "false"
		}
		if vs := req.Form["schemes"]; len(vs) > 0 {
			requested := strings.Split(vs[0], ",")
			conf.ExternalSchemes = []string{}
			for _, scheme := range requested {
				// Only allow http and https
				if scheme == "http" || scheme == "https" {
					conf.ExternalSchemes = append(conf.ExternalSchemes, scheme)
				} else {
					log.Warnf("Excluded unknown scheme: %s", scheme)
				}
			}
		}
		swaggerGen = openapi.NewABI2Swagger(&conf)
	}
	return
}

func (g *smartContractGW) replyWithSwagger(res http.ResponseWriter, req *http.Request, swagger *spec.Swagger, id, from string) {
	if from != "" {
		if swagger.Parameters != nil {
			if param, exists := swagger.Parameters["fromParam"]; exists {
				param.SimpleSchema.Default = from
				swagger.Parameters["fromParam"] = param
			}
		}
	}
	swaggerBytes, _ := json.MarshalIndent(&swagger, "", "  ")

	log.Infof("<-- %s %s [%d]", req.Method, req.URL, 200)
	res.Header().Set("Content-Type", "application/json")
	if vs := req.Form["download"]; len(vs) > 0 {
		res.Header().Set("Content-Disposition", "attachment; filename=\""+id+".swagger.json\"")
	}
	res.WriteHeader(200)
	res.Write(swaggerBytes)
}

func (g *smartContractGW) getContractOrABI(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)
	swaggerGen, uiRequest, factoryOnly, abiRequest, _, from := g.isSwaggerRequest(req)
	id := strings.TrimPrefix(strings.ToLower(params.ByName("address")), "0x")
	prefix := "contract"
	if id == "" {
		id = strings.ToLower(params.ByName("abi"))
		prefix = "abi"
	}
	// For safety we always check our sanitized address index in memory, before checking the filesystem
	var registeredName string
	var err error
	var deployMsg *messages.DeployContract
	var info messages.TimeSortable
	var abiID string
	if prefix == "contract" {
		if deployMsg, registeredName, info, err = g.resolveAddressOrName(params.ByName("address")); err != nil {
			g.gatewayErrReply(res, req, err, 404)
			return
		}
	} else {
		abiID = id
		info, err = g.cs.GetLocalABIInfo(abiID)
		if err == nil {
			var result *contractregistry.DeployContractWithAddress
			result, err = g.cs.GetABI(contractregistry.ABILocation{
				ABIType: contractregistry.LocalABI,
				Name:    abiID,
			}, false)
			if result != nil {
				deployMsg = result.Contract
			}
		}
		if err != nil {
			g.gatewayErrReply(res, req, err, 404)
			return
		}
	}
	if uiRequest {
		g.writeHTMLForUI(prefix, id, from, (prefix == "abi"), factoryOnly, res)
	} else if swaggerGen != nil {
		addr := params.ByName("address")
		runtimeABI, err := ethbind.API.ABIMarshalingToABIRuntime(deployMsg.ABI)
		if err != nil {
			g.gatewayErrReply(res, req, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayInvalidABI, err), 404)
			return
		}
		swagger := g.swaggerForABI(swaggerGen, abiID, deployMsg.ContractName, factoryOnly, runtimeABI, deployMsg.DevDoc, addr, registeredName)
		g.replyWithSwagger(res, req, swagger, id, from)
	} else if abiRequest {
		log.Infof("<-- %s %s [%d]", req.Method, req.URL, 200)
		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(200)
		enc := json.NewEncoder(res)
		enc.SetIndent("", "  ")
		enc.Encode(deployMsg.ABI)
	} else {
		log.Infof("<-- %s %s [%d]", req.Method, req.URL, 200)
		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(200)
		enc := json.NewEncoder(res)
		enc.SetIndent("", "  ")
		enc.Encode(info)
	}
}

func (g *smartContractGW) getRemoteRegistrySwaggerOrABI(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	swaggerGen, uiRequest, factoryOnly, abiRequest, refreshABI, from := g.isSwaggerRequest(req)

	var msg *contractregistry.DeployContractWithAddress
	var deployMsg *messages.DeployContract
	var err error
	var isGateway = false
	var prefix, id, addr string
	if strings.HasPrefix(req.URL.Path, "/gateways/") || strings.HasPrefix(req.URL.Path, "/g/") {
		isGateway = true
		prefix = "gateway"
		id = params.ByName("gateway_lookup")
		msg, err = g.cs.GetABI(contractregistry.ABILocation{
			ABIType: contractregistry.RemoteGateway,
			Name:    id,
		}, refreshABI)
		if err != nil {
			g.gatewayErrReply(res, req, err, 500)
			return
		} else if msg == nil || msg.Contract == nil {
			err = ethconnecterrors.Errorf(ethconnecterrors.RemoteRegistryLookupGatewayNotFound)
			g.gatewayErrReply(res, req, err, 404)
			return
		}
		deployMsg = msg.Contract
	} else {
		prefix = "instance"
		id = params.ByName("instance_lookup")
		msg, err = g.cs.GetABI(contractregistry.ABILocation{
			ABIType: contractregistry.RemoteInstance,
			Name:    id,
		}, refreshABI)
		if err != nil {
			g.gatewayErrReply(res, req, err, 500)
			return
		} else if msg == nil || msg.Contract == nil {
			err = ethconnecterrors.Errorf(ethconnecterrors.RemoteRegistryLookupInstanceNotFound)
			g.gatewayErrReply(res, req, err, 404)
			return
		}
		deployMsg = msg.Contract
		addr = msg.Address
	}

	if uiRequest {
		g.writeHTMLForUI(prefix, id, from, isGateway, factoryOnly, res)
	} else if swaggerGen != nil {
		runtimeABI, err := ethbind.API.ABIMarshalingToABIRuntime(deployMsg.ABI)
		if err != nil {
			g.gatewayErrReply(res, req, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayInvalidABI, err), 400)
			return
		}
		swagger := g.swaggerForRemoteRegistry(swaggerGen, id, addr, factoryOnly, runtimeABI, deployMsg.DevDoc, req.URL.Path)
		g.replyWithSwagger(res, req, swagger, id, from)
	} else if abiRequest {
		log.Infof("<-- %s %s [%d]", req.Method, req.URL, 200)
		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(200)
		enc := json.NewEncoder(res)
		enc.SetIndent("", "  ")
		enc.Encode(deployMsg.ABI)
	} else {
		ci := &remoteContractInfo{
			ID:      deployMsg.Headers.ID,
			ABI:     deployMsg.ABI,
			Address: addr,
		}
		log.Infof("<-- %s %s [%d]", req.Method, req.URL, 200)
		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(200)
		enc := json.NewEncoder(res)
		enc.SetIndent("", "  ")
		enc.Encode(ci)
	}
}

func (g *smartContractGW) registerContract(res http.ResponseWriter, req *http.Request, params httprouter.Params) {
	log.Infof("--> %s %s", req.Method, req.URL)

	addrHexNo0x := strings.ToLower(strings.TrimPrefix(params.ByName("address"), "0x"))
	addrCheck, _ := regexp.Compile("^[0-9a-z]{40}$")
	if !addrCheck.MatchString(addrHexNo0x) {
		g.gatewayErrReply(res, req, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayRegistrationSuppliedInvalidAddress), 404)
		return
	}

	// Note: there is currently no body payload required for the POST

	abiID := params.ByName("abi")
	_, err := g.cs.GetABI(contractregistry.ABILocation{
		ABIType: contractregistry.LocalABI,
		Name:    abiID,
	}, false)
	if err != nil {
		g.gatewayErrReply(res, req, err, 404)
		return
	}

	registerAs := getFlyParam("register", req)
	registeredName := registerAs
	if registeredName == "" {
		registeredName = addrHexNo0x
	}

	contractInfo, err := g.cs.AddContract(addrHexNo0x, abiID, registeredName, registerAs)
	if err != nil {
		g.gatewayErrReply(res, req, err, 409)
		return
	}

	status := 201
	log.Infof("<-- %s %s [%d]", req.Method, req.URL, status)
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(status)
	json.NewEncoder(res).Encode(&contractInfo)
}

func tempdir() string {
	dir, _ := ioutil.TempDir("", "fly")
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
		g.gatewayErrReply(res, req, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractInvalidFormData, err), 400)
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

	if vs := req.Form["findsolidity"]; len(vs) > 0 {
		var solFiles []string
		filepath.Walk(
			tempdir,
			func(p string, info os.FileInfo, err error) error {
				if strings.HasSuffix(p, ".sol") {
					solFiles = append(solFiles, strings.TrimPrefix(strings.TrimPrefix(p, tempdir), "/"))
				}
				return nil
			})
		log.Infof("<-- %s %s [%d]", req.Method, req.URL, 200)
		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(200)
		json.NewEncoder(res).Encode(&solFiles)
		return
	}

	abi, err := g.parseABI(req.Form)
	if err != nil {
		g.gatewayErrReply(res, req, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractInvalidFormData, err), 400)
		return
	}

	bytecode, err := g.parseBytecode(req.Form)
	if err != nil {
		g.gatewayErrReply(res, req, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractInvalidFormData, err), 400)
		return
	}

	var preCompiled map[string]*ethbinding.Contract
	if bytecode == nil {
		var err error
		preCompiled, err = g.compileMultipartFormSolidity(tempdir, req)
		if err != nil {
			g.gatewayErrReply(res, req, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractCompileFailed, err), 400)
			return
		}
	}

	if vs := req.Form["findcontracts"]; len(vs) > 0 {
		contractNames := make([]string, 0, len(preCompiled))
		for contractName := range preCompiled {
			contractNames = append(contractNames, contractName)
		}
		log.Infof("<-- %s %s [%d]", req.Method, req.URL, 200)
		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(200)
		json.NewEncoder(res).Encode(&contractNames)
		return
	}

	msg := &messages.DeployContract{}
	msg.Headers.MsgType = messages.MsgTypeSendTransaction
	msg.Headers.ID = utils.UUIDv4()
	var compiled *eth.CompiledSolidity
	if bytecode == nil && abi == nil {
		var err error
		compiled, err = eth.ProcessCompiled(preCompiled, req.FormValue("contract"), false)
		if err != nil {
			g.gatewayErrReply(res, req, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractPostCompileFailed, err), 400)
			return
		}
	} else {
		msg.ABI = abi
		msg.Compiled = bytecode
	}

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

func (g *smartContractGW) parseBytecode(form url.Values) ([]byte, error) {
	v := form["bytecode"]
	if len(v) > 0 {
		b := strings.TrimLeft(v[0], "0x")
		if bytecode, err := hex.DecodeString(b); err != nil {
			log.Errorf("failed to decode hex string: %v", err)
			return nil, err
		} else {
			return bytecode, nil
		}
	}
	return nil, nil
}

func (g *smartContractGW) parseABI(form url.Values) (ethbinding.ABIMarshaling, error) {
	v := form["abi"]
	if len(v) > 0 {
		a := v[0]
		var abi ethbinding.ABIMarshaling
		if err := json.Unmarshal([]byte(a), &abi); err != nil {
			log.Errorf("failed to unmarshal ABI: %v", err.Error())
			return nil, err
		} else {
			return abi, nil
		}
	}
	return nil, nil
}

func (g *smartContractGW) compileMultipartFormSolidity(dir string, req *http.Request) (map[string]*ethbinding.Contract, error) {
	solFiles := []string{}
	rootFiles, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Errorf("Failed to read dir '%s': %s", dir, err)
		return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractExtractedReadFailed)
	}
	for _, file := range rootFiles {
		log.Debugf("multi-part: '%s' [dir=%t]", file.Name(), file.IsDir())
		if strings.HasSuffix(file.Name(), ".sol") {
			solFiles = append(solFiles, file.Name())
		}
	}

	evmVersion := req.FormValue("evm")
	solcArgs := eth.GetSolcArgs(evmVersion)
	if sourceFiles := req.Form["source"]; len(sourceFiles) > 0 {
		solcArgs = append(solcArgs, sourceFiles...)
	} else if len(solFiles) > 0 {
		solcArgs = append(solcArgs, solFiles...)
	} else {
		return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractNoSOL)
	}

	solcVer, err := eth.GetSolc(req.FormValue("compiler"))
	if err != nil {
		return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractSolcVerFail, err)
	}
	solOptionsString := strings.Join(append([]string{solcVer.Path}, solcArgs...), " ")
	log.Infof("Compiling: %s", solOptionsString)
	cmd := exec.Command(solcVer.Path, solcArgs...)

	var stderr, stdout bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractCompileFailDetails, err, stderr.String())
	}

	compiled, err := ethbind.API.ParseCombinedJSON(stdout.Bytes(), "", solcVer.Version, solcVer.Version, solOptionsString)
	if err != nil {
		return nil, ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractSolcOutputProcessFail, err)
	}

	return compiled, nil
}

func (g *smartContractGW) extractMultiPartFile(dir string, file *multipart.FileHeader) error {
	fileName := file.Filename
	if strings.ContainsAny(fileName, "/\\") {
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractSlashes)
	}
	in, err := file.Open()
	if err != nil {
		log.Errorf("Failed opening '%s' for reading: %s", fileName, err)
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractUnzipRead)
	}
	defer in.Close()
	outFileName := path.Join(dir, fileName)
	out, err := os.OpenFile(outFileName, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Errorf("Failed opening '%s' for writing: %s", fileName, err)
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractUnzipWrite)
	}
	written, err := io.Copy(out, in)
	if err != nil {
		log.Errorf("Failed writing '%s' from multi-part form: %s", fileName, err)
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractUnzipCopy)
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
		return ethconnecterrors.Errorf(ethconnecterrors.RESTGatewayCompileContractUnzip, err)
	}
	return nil
}

// Write out a nice little UI for exercising the Swagger
func (g *smartContractGW) writeHTMLForUI(prefix, id, from string, isGateway, factoryOnly bool, res http.ResponseWriter) {
	fromQuery := ""
	if from != "" {
		fromQuery = "&from=" + url.QueryEscape(from)
	}

	factoryMessage := ""
	if isGateway {
		factoryMessage =
			`       <li><code>POST</code> against <code>/</code> (the constructor) will deploy a new instance of the smart contract
        <ul>
          <li>A dedicated API will be generated for each instance deployed via this API, scoped to that contract Address</li>
        </ul></li>`
	}
	factoryOnlyQuery := ""
	helpHeader := `
  <p>Welcome to the built-in API exerciser of Ethconnect</p>
  `
	hasMethodsMessage := ""
	if factoryOnly {
		factoryOnlyQuery = "&factory"
		helpHeader = `<p>Factory API to deploy contract instances</p>
  <p>Use the <code>[POST]</code> panel below to set the input parameters for your constructor, and tick <code>[TRY]</code> to deploy a contract instance.</p>
  <p>If you want to configure a friendly API path name to invoke your contract, then set the <code>fly-register</code> parameter.</p>`
	} else {
		hasMethodsMessage = `<li><code>GET</code> actions <b>never</b> write to the chain. Even for actions that update state - so you can simulate execution</li>
    <li><code>POST</code> actions against <code>/subscribe</code> paths marked <code>[event]</code> add subscriptions to event streams
    <ul>
      <li>Pre-configure your event streams with actions via the <code>/eventstreams</code> API route on Ethconnect</b></li>
      <li>Once you add a subscription, all matching events will be reliably read, batched and delivered over your event stream</li>
    </ul></li>
    <li>Data type conversion is automatic for all actions an events.
      <ul>
          <li>Numbers are encoded as strings, to avoid loss of precision.</li>
          <li>Byte arrays, including Address fields, are encoded in Hex with an <code>0x</code> prefix</li>
          <li>See the 'Model' of each method and event input/output below for details</li>
      </ul>
    </li>`
	}
	html := `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd">
<html>
<head>
  <meta charset="utf-8"> <!-- Important: rapi-doc uses utf8 characters -->
  <script src="https://unpkg.com/rapidoc@7.1.0/dist/rapidoc-min.js"></script>
</head>
<body>
  <rapi-doc 
    spec-url="` + g.conf.BaseURL + "/" + prefix + "s/" + id + "?swagger" + factoryOnlyQuery + fromQuery + `"
    allow-authentication="false"
    allow-spec-url-load="false"
    allow-spec-file-load="false"
    heading-text="Ethconnect REST Gateway"
    header-color="#3842C1"
    theme="light"
		primary-color="#3842C1"
  >
<!-- TODO new image and docs link
    <img 
      slot="logo" 
      src="todo"
      alt="Firefly"
      onclick="window.open('https://todo')"
      style="cursor: pointer; padding-bottom: 2px; margin-left: 25px; margin-right: 10px;"
    />
-->
    <div style="border: #f2f2f2 1px solid; padding: 25px; margin-top: 25px;
      display: flex; flex-direction: row; flex-wrap: wrap;">
      <div style="flex: 1;">
      ` + helpHeader + `
        <p><a href="#quickstart" style="text-decoration: none" onclick="document.getElementById('firefly-quickstart-header').style.display = 'block'; this.style.display = 'none'; return false;">Show additional instructions</a></p>
        <div id="firefly-quickstart-header" style="display: none;">
          <ul>
            <li>Authorization with Firefly Application Credentials has already been performed when loading this page, and is passed to API calls by your browser.</code>
            <li><code>POST</code> actions against Solidity methods will <b>write to the chain</b> unless <code>fly-call</code> is set, or the method is marked <code>[read-only]</code>
            <ul>
              <li>When <code>fly-sync</code> is set, the response will not be returned until the transaction is mined <b>taking a few seconds</b></li>
              <li>When <code>fly-sync</code> is unset, the transaction is reliably streamed to the node over Kafka</li>
              <li>Use the <a href="/replies" target="_blank" style="text-decoration: none">/replies</a> API route on Ethconnect to view receipts for streamed transactions</li>
              <li>Gas limit estimation is performed automatically, unless <code>fly-gas</code> is set.</li>
              <li>During the gas estimation we will return any revert messages if there is a execution failure.</li>
            </ul></li>
            ` + factoryMessage + `
            ` + hasMethodsMessage + `
            <li>Descriptions are taken from the devdoc included in the Solidity code comments</li>
          </ul>        
        </div>
      </div>
      <div style="flex-shrink: 1; margin-left: auto; text-align: center;"">
        <button type="button" style="color: white; background-color: #3942c1;
          font-size: 1rem; border-radius: 4px; cursor: pointer;
          text-transform: uppercase; height: 50px; padding: 0 20px;
          text-align: center; box-sizing: border-box; margin-bottom: 10px;"
          onclick="window.open('` + g.conf.BaseURL + "/" + prefix + "s/" + id + "?swagger&download" + fromQuery + `')">
          Download API
        </button><br/>
<!-- TODO new docs link -->
      </div>
    </div>
  </rapi-doc>
</body> 
</html>
`
	res.Header().Set("Content-Type", "text/html; charset=utf-8")
	res.WriteHeader(200)
	res.Write([]byte(html))
}

// Shutdown performs a clean shutdown
func (g *smartContractGW) Shutdown() {
	if g.sm != nil {
		g.sm.Close(false)
	}
	if g.cs != nil {
		g.cs.Close()
	}
}

func (g *smartContractGW) resolveAddressOrName(id string) (deployMsg *messages.DeployContract, registeredName string, info *contractregistry.ContractInfo, err error) {
	info, err = g.cs.GetContractByAddress(id)
	if err != nil {
		var origErr = err
		registeredName = id
		if id, err = g.cs.ResolveContractAddress(registeredName); err != nil {
			log.Infof("%s is not a friendly name: %s", registeredName, err)
			return nil, "", nil, origErr
		}
		if info, err = g.cs.GetContractByAddress(id); err != nil {
			return nil, "", nil, err
		}
	}
	result, err := g.cs.GetABI(contractregistry.ABILocation{
		ABIType: contractregistry.LocalABI,
		Name:    info.ABI,
	}, false)
	if result != nil {
		deployMsg = result.Contract
	}
	return deployMsg, registeredName, info, err
}
