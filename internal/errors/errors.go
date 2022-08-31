// Copyright 2019,2020 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package errors

import (
	"fmt"
)

var errDupCheck = map[int]bool{}

type ErrorID interface {
	Code() string
}

type errorID struct {
	code  int
	enMsg string
}

func (e *errorID) Code() string {
	return fmt.Sprintf("FFEC%d", e.code)
}

func e(code int, enMsg string) ErrorID {
	if _, ok := errDupCheck[code]; ok {
		panic(fmt.Sprintf("Duplicate code %d: %s", code, enMsg))
	}
	if code < 100000 || code > 200000 {
		panic(fmt.Sprintf("Invalid code %d: %s", code, enMsg))
	}
	e := &errorID{code, enMsg}
	errDupCheck[code] = true
	return e
}

var (

	// AddressBookLookupBadURL we got back a bad URL from the remote address book after our REST call
	AddressBookLookupBadURL = e(100000, "Invalid URL obtained for address")
	// AddressBookLookupBadHostsFile we have a custom hosts file for DNS resolution, but it cannot be processed
	AddressBookLookupBadHostsFile = e(100001, "Configuration problem (hosts file)")
	// AddressBookLookupNotFound remote addressbook says no
	AddressBookLookupNotFound = e(100002, "Unknown address")

	// ConfigFileReadFailed failed to read the server config file
	ConfigFileReadFailed = e(100003, "Failed to read %s: %s")
	// CompilerVersionNotFound the runtime context of ethconnect has not been configured with a compiler for the requested version
	CompilerVersionNotFound = e(100004, "Could not find a configured compiler for requested Solidity major version %s.%s")
	// CompilerVersionBadRequest the user requested a bad semver
	CompilerVersionBadRequest = e(100005, "Invalid Solidity version requested for compiler. Ensure the string starts with two dot separated numbers, such as 0.5")
	// CompilerFailedSolc compilation failure output from solc
	CompilerFailedSolc = e(100006, "Solidity compilation failed: solc: %v\n%s")
	// CompilerOutputMissingContract the output from the compiler does not include the requested contract
	CompilerOutputMissingContract = e(100007, "Contract '%s' not found in Solidity source: %s")
	// CompilerOutputMultipleContracts need to select one
	CompilerOutputMultipleContracts = e(100008, "More than one contract in Solidity file, please set one to call: %s")
	// CompilerBytecodeInvalid hex output from compiler could not be parsed
	CompilerBytecodeInvalid = e(100009, "Decoding bytecode: %s")
	// CompilerBytecodeEmpty null result from succcessful compile in solc
	CompilerBytecodeEmpty = e(100010, "Specified contract compiled ok, but did not result in any bytecode: %s")
	// CompilerABISerialize could not serialize the ABI output from solc
	CompilerABISerialize = e(100011, "Serializing ABI: %s")
	// CompilerABIReRead could not re-read serialized output after writing the ABI
	CompilerABIReRead = e(100012, "Parsing ABI: %s")
	// CompilerSerializeDevDocs could not serialize the dev docs output from solc
	CompilerSerializeDevDocs = e(100013, "Serializing DevDoc: %s")
	// ConfigNoRPC missing config for JSON/RPC
	ConfigNoRPC = e(100014, "No JSON/RPC URL set for ethereum node")
	// ConfigKafkaMissingOutputTopic response topic missing
	ConfigKafkaMissingOutputTopic = e(100015, "No output topic specified for bridge to send events to")
	// ConfigKafkaMissingInputTopic request topic missing
	ConfigKafkaMissingInputTopic = e(100016, "No input topic specified for bridge to listen to")
	// ConfigKafkaMissingConsumerGroup consumer group missing
	ConfigKafkaMissingConsumerGroup = e(100017, "No consumer group specified")
	// ConfigKafkaMissingBadSASL problem with SASL config
	ConfigKafkaMissingBadSASL = e(100018, "Username and Password must both be provided for SASL")
	// ConfigKafkaMissingBrokers missing/empty brokers
	ConfigKafkaMissingBrokers = e(100019, "No Kafka brokers configured")
	// ConfigRESTGatewayRequiredReceiptStore need to enable params for REST Gatewya
	ConfigRESTGatewayRequiredReceiptStore = e(100020, "MongoDB URL, Database and Collection name must be specified to enable the receipt store")
	// ConfigRESTGatewayRequiredRPC and RPC stuff
	ConfigRESTGatewayRequiredRPC = e(100021, "RPC URL and Storage Path must be supplied to enable the Open API REST Gateway")
	// ConfigWebhooksDirectRPC for webhooks direct
	ConfigWebhooksDirectRPC = e(100022, "No JSON/RPC URL set for ethereum node")
	// ConfigTLSCertOrKey incomplete TLS config
	ConfigTLSCertOrKey = e(100023, "Client private key and certificate must both be provided for mutual auth")

	// ConfigNoYAML missing configuration file on server start
	ConfigNoYAML = e(100024, "No YAML configuration filename specified")
	// ConfigYAMLParseFile failed to parse YAML during server startup
	ConfigYAMLParseFile = e(100025, "Unable to parse %s as YAML: %s")
	// ConfigYAMLPostParseFile failed to process YAML as JSON after parsing
	ConfigYAMLPostParseFile = e(100026, "Failed to process YAML config from %s: %s")

	// DeployTransactionMissingCode a DeployTransaction message, without code to deploy
	DeployTransactionMissingCode = e(100027, "Missing Compiled Code + ABI, or Solidity")

	// EventStreamsDBLoad failed to init DB
	EventStreamsDBLoad = e(100028, "Failed to open DB at %s: %s")
	// EventStreamsNoID attempt to create an event stream/sub without an ID
	EventStreamsNoID = e(100029, "No ID")
	// EventStreamsInvalidActionType unknown action type
	EventStreamsInvalidActionType = e(100030, "Unknown action type '%s'")
	// EventStreamsWebhookNoURL attempt to create a Webhook event stream without a URL
	EventStreamsWebhookNoURL = e(100031, "Must specify webhook.url for action type 'webhook'")
	// EventStreamsWebhookInvalidURL attempt to create a Webhook event stream with an invalid URL
	EventStreamsWebhookInvalidURL = e(100032, "Invalid URL in webhook action")
	// EventStreamsWebhookResumeActive resume when already resumed
	EventStreamsWebhookResumeActive = e(100033, "Event processor is already active. Suspending:%t")
	// EventStreamsWebhookProhibitedAddress some IP ranges can be restricted
	EventStreamsWebhookProhibitedAddress = e(100034, "Cannot send Webhook POST to address: %s")
	// EventStreamsWebhookFailedHTTPStatus server at the other end of a webhook returned a non-OK response
	EventStreamsWebhookFailedHTTPStatus = e(100035, "%s: Failed with status=%d")
	// EventStreamsSubscribeBadBlock the starting block for a subscription request is invalid
	EventStreamsSubscribeBadBlock = e(100036, "FromBlock cannot be parsed as a BigInt")
	// EventStreamsSubscribeStoreFailed problem saving a subscription to our DB
	EventStreamsSubscribeStoreFailed = e(100037, "Failed to store subscription: %s")
	// EventStreamsSubscribeNoEvent missing event
	EventStreamsSubscribeNoEvent = e(100038, "Solidity event name must be specified")
	// EventStreamsSubscriptionNotFound sub not found
	EventStreamsSubscriptionNotFound = e(100039, "Subscription with ID '%s' not found")
	// EventStreamsCreateStreamStoreFailed problem saving a subscription to our DB
	EventStreamsCreateStreamStoreFailed = e(100040, "Failed to store stream: %s")
	// EventStreamsCreateStreamResourceErr problem creating a resource required by the eventstream
	EventStreamsCreateStreamResourceErr = e(100041, "Failed to create a resource for the stream: %s")
	// EventStreamsStreamNotFound stream not found
	EventStreamsStreamNotFound = e(100042, "Stream with ID '%s' not found")
	// EventStreamsLogDecode problem decoding the logs for an event emitted on the chain
	EventStreamsLogDecode = e(100043, "%s: Failed to decode data: %s")
	// EventStreamsLogDecodeInsufficientTopics ran out of topics according to the indexed fields described on the ABI event
	EventStreamsLogDecodeInsufficientTopics = e(100044, "%s: Ran out of topics for indexed fields at field %d of %s")
	// EventStreamsLogDecodeData RLP decoding of the data section of the logs failed
	EventStreamsLogDecodeData = e(100045, "%s: Failed to parse RLP data from event: %s")
	// EventStreamsWebSocketNotConfigured WebSocket not configured
	EventStreamsWebSocketNotConfigured = e(100046, "WebSocket listener not configured")
	// EventStreamsWebSocketInterruptedSend When we are interrupted waiting for a viable connection to send down
	EventStreamsWebSocketInterruptedSend = e(100047, "Interrupted waiting for WebSocket connection to send event")
	// EventStreamsWebSocketInterruptedReceive When we are interrupted waiting for a viable connection to send down
	EventStreamsWebSocketInterruptedReceive = e(100048, "Interrupted waiting for WebSocket acknowledgment")
	// EventStreamsWebSocketErrorFromClient Error message received from client
	EventStreamsWebSocketErrorFromClient = e(100049, "Error received from WebSocket client: %s")
	// EventStreamsCannotUpdateType cannot change tyep
	EventStreamsCannotUpdateType = e(100050, "The type of an event stream cannot be changed")
	// EventStreamsInvalidDistributionMode unknown distribution mode
	EventStreamsInvalidDistributionMode = e(100051, "Invalid distribution mode '%s'. Valid distribution modes are: 'workloadDistribution' and 'broadcast'.")
	// EventStreamsUpdateAlreadyInProgress update already in progress
	EventStreamsUpdateAlreadyInProgress = e(100052, "Update to event stream already in progress")

	// KakfaProducerConfirmMsgUnknown we received a confirmation callback, but we aren't expecting it
	KakfaProducerConfirmMsgUnknown = e(100053, "Received confirmation for message not in in-flight map: %s")

	// KVStoreDBLoad failed to init DB
	KVStoreDBLoad = e(100054, "Failed to open DB at %s: %s")
	// KVStoreMemFilteringUnsupported memory db is really just for testing. No filtering support
	KVStoreMemFilteringUnsupported = e(100055, "Memory receipts do not support filtering")

	// HDWalletSigningFailed problem returned from remote HDWallet API
	HDWalletSigningFailed = e(100056, "HDWallet signing failed")
	// HDWalletSigningBadData we got a response, but not with the correct fields
	HDWalletSigningBadData = e(100057, "Unexpected response from HDWallet")
	// HDWalletSigningNoConfig we had a request for HD Wallet signing, but we don't have the required config
	HDWalletSigningNoConfig = e(100058, "No HD Wallet Configuration")

	// HelperStrToAddressRequiredField re-usable error for missing fields
	HelperStrToAddressRequiredField = e(100059, "'%s' must be supplied")
	// HelperStrToAddressBadAddress re-usable error for bad address
	HelperStrToAddressBadAddress = e(100060, "Supplied value for '%s' is not a valid hex address")
	// HelperYAMLorJSONPayloadTooLarge input message too large
	HelperYAMLorJSONPayloadTooLarge = e(100061, "Message exceeds maximum allowable size")
	// HelperYAMLorJSONPayloadReadFailed failed to read input
	HelperYAMLorJSONPayloadReadFailed = e(100062, "Unable to read input data: %s")
	// HelperYAMLorJSONPayloadParseFailed input message got error parsing
	HelperYAMLorJSONPayloadParseFailed = e(100063, "Unable to parse as YAML or JSON: %s")

	// HTTPRequesterSerializeFailed common HTTP request utility for extensions, failed to serialize request
	HTTPRequesterSerializeFailed = e(100064, "Failed to serialize request payload: %s")
	// HTTPRequesterNonStatusError common HTTP request utility for extensions, got an error sending a request
	HTTPRequesterNonStatusError = e(100065, "Error querying %s")
	// HTTPRequesterStatusErrorNoData common HTTP request utility for extensions, got a status code, but couldn't deserialize payload
	HTTPRequesterStatusErrorNoData = e(100066, "Could not process %s [%d] response")
	// HTTPRequesterStatusErrorWithData common HTTP request utility for extensions, got a non-ok status code with JSON errorMessage
	HTTPRequesterStatusErrorWithData = e(100067, "%s returned [%d]: %s")
	// HTTPRequesterStatusError common HTTP request utility for extensions, got a non-ok status code
	HTTPRequesterStatusError = e(100068, "Error querying %s")
	// HTTPRequesterResponseMissingField common HTTP request utility for extensions, missing expected field in response
	HTTPRequesterResponseMissingField = e(100069, "'%s' missing in %s response")
	// HTTPRequesterResponseNonStringField common HTTP request utility for extensions, expected string for field in response
	HTTPRequesterResponseNonStringField = e(100070, "'%s' not a string in %s response")
	// HTTPRequesterResponseNullField common HTTP request utility for extensions, expected non-empty response field
	HTTPRequesterResponseNullField = e(100071, "'%s' empty (or null) in %s response")

	// ReceiptStoreDisabled not configured
	ReceiptStoreDisabled = e(100072, "Receipt store not enabled")
	// ReceiptStoreDBLoad failed to init DB
	ReceiptStoreDBLoad = e(100073, "Failed to open DB at %s: %s")
	// ReceiptStoreMongoDBConnect couldn't connect to MongoDB
	ReceiptStoreMongoDBConnect = e(100074, "Unable to connect to MongoDB: %s")
	// ReceiptStoreMongoDBIndex couldn't create MongoDB index
	ReceiptStoreMongoDBIndex = e(100075, "Unable to create index: %s")
	// ReceiptStoreLevelDBConnect couldn't open file for the level DB
	ReceiptStoreLevelDBConnect = e(100076, "Unable to open LevelDB: %s")
	// ReceiptStoreSerializeResponse problem sending a receipt stored back over the REST API
	ReceiptStoreSerializeResponse = e(100077, "Error serializing response")
	// ReceiptStoreInvalidRequestID bad ID query
	ReceiptStoreInvalidRequestID = e(100078, "Invalid 'id' query parameter")
	// ReceiptStoreInvalidRequestMaxLimit bad limit over max
	ReceiptStoreInvalidRequestMaxLimit = e(100079, "Maximum limit is %d")
	// ReceiptStoreInvalidRequestBadLimit bad limit
	ReceiptStoreInvalidRequestBadLimit = e(100080, "Invalid 'limit' query parameter")
	// ReceiptStoreInvalidRequestBadSkip bad skip
	ReceiptStoreInvalidRequestBadSkip = e(100081, "Invalid 'skip' query parameter")
	// ReceiptStoreInvalidRequestBadSince bad since
	ReceiptStoreInvalidRequestBadSince = e(100082, "since cannot be parsed as RFC3339 or millisecond timestamp")
	// ReceiptStoreFailedQuery wrapper over detailed error
	ReceiptStoreFailedQuery = e(100083, "Error querying replies: %s")
	// ReceiptStoreFailedQuerySingle wrapper over detailed error
	ReceiptStoreFailedQuerySingle = e(100084, "Error querying reply: %s")
	// ReceiptStoreFailedNotFound receipt isn't in the store
	ReceiptStoreFailedNotFound = e(100085, "Receipt not available")

	// RemoteRegistryCacheInit initialzation issue for remote contract registry
	RemoteRegistryCacheInit = e(100086, "Failed to initialize cache for remote registry: %s")
	// RemoteRegistryNotConfigured cannot register as a remote registry is not configured
	RemoteRegistryNotConfigured = e(100087, "No remote registry is configured")
	// RemoteRegistryRegistrationFailed error during registration with remote contract registry
	RemoteRegistryRegistrationFailed = e(100088, "Failed to register instance in remote registry: %s")
	// RemoteRegistryLookupGatewayNotFound did not find the requested ID in the remote registry for a gateway/factory
	RemoteRegistryLookupGatewayNotFound = e(100089, "Gateway not found")
	// RemoteRegistryLookupInstanceNotFound did not find the requested ID in the remote registry for a contract instance
	RemoteRegistryLookupInstanceNotFound = e(100090, "Instance not found")
	// RemoteRegistryLookupGenericProcessingFailed we don't return the full original error over the REST API after logging
	RemoteRegistryLookupGenericProcessingFailed = e(100091, "Error processing contract registry response")

	// RESTGatewayGatewayNotFound the gateway REST API interface (the 'factory' / ABI generic interface) was not found
	RESTGatewayGatewayNotFound = e(100092, "Gateway not found")
	// RESTGatewayInstanceNotFound the instance REST API interface (an individual registered address) was not found
	RESTGatewayInstanceNotFound = e(100093, "Instance not found")
	// RESTGatewayEventNotDeclared attempt to subscribe to an event on an instance that does not exist
	RESTGatewayEventNotDeclared = e(100094, "Event '%s' is not declared in the ABI")
	// RESTGatewayMethodNotDeclared attempt to invoke a method name that does not exist in the ABI, or register globally for an event that doesn't exist
	RESTGatewayMethodNotDeclared = e(100095, "Method or Event '%s' is not declared in the ABI of contract '%s'")
	// RESTGatewayInvalidToAddress failed to parse a 'to' address supplied on a path
	RESTGatewayInvalidToAddress = e(100096, "To Address must be a 40 character hex string (0x prefix is optional)")
	// RESTGatewayInvalidFromAddress failed to parse a 'from' address supplied on a path
	RESTGatewayInvalidFromAddress = e(100097, "From Address must be a 40 character hex string (0x prefix is optional)")
	// RESTGatewayMissingParameter did not supply a parameter required by the method
	RESTGatewayMissingParameter = e(100098, "Parameter '%s' of method '%s' was not specified in body or query parameters")
	// RESTGatewayMissingFromAddress did not supply a signing address for the transaction
	RESTGatewayMissingFromAddress = e(100099, "Please specify a valid address in the '%[1]s-from' query string parameter or x-%[2]s-from HTTP header")
	// RESTGatewaySubscribeMissingStreamParameter missed the ID of the stream when registering
	RESTGatewaySubscribeMissingStreamParameter = e(100100, "Must supply a 'stream' parameter in the body or query")
	// RESTGatewayMixedPrivateForAndGroupID confused privacy group info, using simple/Tessera style as well as pre-defined/Orion style
	RESTGatewayMixedPrivateForAndGroupID = e(100101, "%[1]s-privatefor and %[1]s-privacygroupid are mutually exclusive")
	// RESTGatewayEventManagerInitFailed constructor failure for event manager
	RESTGatewayEventManagerInitFailed = e(100102, "Event-stream subscription manager: %s")
	// RESTGatewayEventStreamInvalid attempt to create an event stream with invalid parameters
	RESTGatewayEventStreamInvalid = e(100103, "Invalid event stream specification: %s")
	// RESTGatewayPostDeployMissingAddress after deployment the receipt did not contain a contract address
	RESTGatewayPostDeployMissingAddress = e(100104, "%s: Missing contract address in receipt")
	// RESTGatewayRegistrationSuppliedInvalidAddress invalid address when registering an existing instance of a contract
	RESTGatewayRegistrationSuppliedInvalidAddress = e(100105, "Invalid address in path - must be a 40 character hex string with optional 0x prefix")
	// RESTGatewaySyncMsgTypeMismatch sync-invoke code paths in REST API Gateway should be maintained such that this cannot happen
	RESTGatewaySyncMsgTypeMismatch = e(100106, "Unexpected condition (message types do not match when processing)")
	// RESTGatewaySyncWrapErrorWithTXDetail wraps a low level error with transaction hash context on sync APIs before returning
	RESTGatewaySyncWrapErrorWithTXDetail = e(100107, "TX %s: %s")
	// RESTGatewayMethodTypeInvalid unsupported method type
	RESTGatewayMethodTypeInvalid = e(100108, "Unsupported method type: %s")
	// RESTGatewayMethodABIInvalid error processing method from ABI
	RESTGatewayMethodABIInvalid = e(100109, "Invalid method '%s' in ABI: %s")
	// RESTGatewayEventABIInvalid error processing method from ABI
	RESTGatewayEventABIInvalid = e(100110, "Invalid event '%s' in ABI: %s")

	// RESTGatewayCompileContractInvalidFormData invalid form data when requesting a compilation to generate an ABI/bytecode
	RESTGatewayCompileContractInvalidFormData = e(100111, "Could not parse supplied multi-part form data: %s")
	// RESTGatewayCompileContractCompileFailed failed to perform compile
	RESTGatewayCompileContractCompileFailed = e(100112, "Failed to compile solidity: %s")
	// RESTGatewayCompileContractPostCompileFailed failed to process output of compilation
	RESTGatewayCompileContractPostCompileFailed = e(100113, "Failed to process solidity: %s")
	// RESTGatewayCompileContractExtractedReadFailed failed to read extracted contents of uploaded data
	RESTGatewayCompileContractExtractedReadFailed = e(100114, "Failed to read extracted multi-part form data")
	// RESTGatewayCompileContractNoSOL failed to find any solidity files in uploaded data
	RESTGatewayCompileContractNoSOL = e(100115, "No .sol files found in root. Please set a 'source' query param or form field to the relative path of your solidity")
	// RESTGatewayCompileContractSolcVerFail failed while checking version of solidity compiler 'solc'
	RESTGatewayCompileContractSolcVerFail = e(100116, "Failed checking solc version: %s")
	// RESTGatewayCompileContractCompileFailDetails output from compiler failure
	RESTGatewayCompileContractCompileFailDetails = e(100117, "Failed to compile [%s]: %s")
	// RESTGatewayCompileContractSolcOutputProcessFail failed to process output of compilation
	RESTGatewayCompileContractSolcOutputProcessFail = e(100118, "Failed to parse solc output: %s")
	// RESTGatewayCompileContractSlashes unsafe slash characters in filenames
	RESTGatewayCompileContractSlashes = e(100119, "Filenames cannot contain slashes. Use a zip file to upload a directory structure")
	// RESTGatewayCompileContractUnzipRead error opening zip/tgz to read (no extra information to remote caller)
	RESTGatewayCompileContractUnzipRead = e(100120, "Failed to read archive")
	// RESTGatewayCompileContractUnzipWrite error writing extracted zip (no extra information to remote caller)
	RESTGatewayCompileContractUnzipWrite = e(100121, "Failed to process archive")
	// RESTGatewayCompileContractUnzipCopy error writing extracted zip (no extra information to remote caller)
	RESTGatewayCompileContractUnzipCopy = e(100122, "Failed to process archive")
	// RESTGatewayCompileContractUnzip failure thrown from decompression library during extract
	RESTGatewayCompileContractUnzip = e(100123, "Error unarchiving supplied zip file: %s")

	// RESTGatewayLocalStoreContractSave local filesystem storage failure for contract instance (non-registry code flow)
	RESTGatewayLocalStoreContractSave = e(100124, "Failed to write ABI JSON: %s")
	// RESTGatewayLocalStoreContractLoad local filesystem load failure for contract instance (non-registry code flow)
	RESTGatewayLocalStoreContractLoad = e(100125, "Failed to find installed contract address for '%s'")
	// RESTGatewayLocalStoreContractNotFound local filesystem not found (non-registry code flow)
	RESTGatewayLocalStoreContractNotFound = e(100126, "No contract instance registered with address %s")
	// RESTGatewayLocalStoreABINotFound lookup of ABI failed not found (non-registry code flow)
	RESTGatewayLocalStoreABINotFound = e(100127, "No ABI found with ID %s")
	// RESTGatewayLocalStoreABILoad local filesystem load failure for ABI details (non-registry code flow)
	RESTGatewayLocalStoreABILoad = e(100128, "Failed to load ABI with ID %s: %s")
	// RESTGatewayLocalStoreABIParse local filesystem parse failure for ABI details (non-registry code flow)
	RESTGatewayLocalStoreABIParse = e(100129, "Failed to parse ABI with ID %s: %s")
	// RESTGatewayLocalStoreMissingABI did not supply ABI JSON when attempting to install ABI (non-registry code flow)
	RESTGatewayLocalStoreMissingABI = e(100130, "Must supply ABI to install an existing ABI into the REST Gateway")
	// RESTGatewayInvalidABI invalid serialized ABI in msg
	RESTGatewayInvalidABI = e(100131, "Invalid ABI: %s")
	// RESTGatewayLocalStoreContractSavePostDeploy local filesystem storage failure for contract instance post deploy (non-registry code flow)
	RESTGatewayLocalStoreContractSavePostDeploy = e(100132, "%s: Failed to write deployment details: %s")
	// RESTGatewayFriendlyNameClash duplicate friendly name when reigstering
	RESTGatewayFriendlyNameClash = e(100133, "Contract address %s is already registered for name '%s'")
	// RESTGatewayResourceErr problem creating a resource required by the gateway
	RESTGatewayResourceErr = e(100134, "Failed to create a resource for the REST Gateway: %s")

	// RPCCallReturnedError specified RPC call returned error
	RPCCallReturnedError = e(100135, "%s returned: %s")
	// RPCConnectFailed error connecting to back-end server over JSON/RPC
	RPCConnectFailed = e(100136, "JSON/RPC connection to %s failed: %s")

	// SecurityModulePluginLoad failed to load .so
	SecurityModulePluginLoad = e(100137, "Failed to load plugin: %s")
	// SecurityModulePluginSymbol missing symbol in plugin
	SecurityModulePluginSymbol = e(100138, "Failed to load 'SecurityModule' symbol from '%s': %s")
	// SecurityModuleNoAuthContext missing auth context in context object at point security module is invoked
	SecurityModuleNoAuthContext = e(100140, "No auth context")

	// TransactionQueryFailed transaction lookup failed
	TransactionQueryFailed = e(100141, "Failed to query transaction: %s")
	// TransactionQueryMethodMismatch transaction input did not match the method queried
	TransactionQueryMethodMismatch = e(100142, "Method signature did not match: %s != %s")

	// TransactionSendConstructorPackArgs RLP encoding failure for a constructor
	TransactionSendConstructorPackArgs = e(100143, "Packing arguments for constructor: %s")
	// TransactionSendMethodPackArgs RLP encoding failure for a method
	TransactionSendMethodPackArgs = e(100144, "Packing arguments for method '%s': %s")
	// TransactionSendInputTypeUnknown there is a type in the ABI inputs that we don't understand
	TransactionSendInputTypeUnknown = e(100145, "ABI input %d: Unable to map %s to etherueum type: %s")
	// TransactionSendOutputTypeUnknown there is a type in the ABI outputs that we don't understand
	TransactionSendOutputTypeUnknown = e(100146, "ABI output %d: Unable to map %s to etherueum type: %s")
	// TransactionSendGasEstimateFailed gas estimation failed prior to sending TX
	TransactionSendGasEstimateFailed = e(100147, "Failed to calculate gas for transaction: %s")
	// TransactionSendCallFailedNoRevert failed to perform an eth_call with a JSON/RPC error (not a revert)
	TransactionSendCallFailedNoRevert = e(100148, "Call failed: %s")
	// TransactionSendCallFailedRevertMessage directly passes the revert message from the EVM
	TransactionSendCallFailedRevertMessage = e(100149, "%s")
	// TransactionSendCallFailedRevertNoMessage when we couldn't process the EVM revert message
	TransactionSendCallFailedRevertNoMessage = e(100150, "EVM reverted. Failed to decode error message")
	// TransactionSendMissingPrivateFromOrion there is no default privateFrom in Orion, so the user must always supply it
	TransactionSendMissingPrivateFromOrion = e(100151, "private-from is required when submitting private transactions via Orion")
	// TransactionSendPrivateTXWithExternalSigner we don't allow private transactions to be combined with a HD Wallet or other external signer currently
	TransactionSendPrivateTXWithExternalSigner = e(100152, "Signing with %s is not currently supported with private transactions")
	// TransactionSendPrivateForAndPrivacyGroup mixed both params
	TransactionSendPrivateForAndPrivacyGroup = e(100153, "privacyGroupId and privateFor are mutually exclusive")
	// TransactionSendNonceFailWithPrivacyGroup when we successfully lookup the privacy group, but cannot get the nonce
	TransactionSendNonceFailWithPrivacyGroup = e(100154, "priv_getTransactionCount for privacy group '%s' returned: %s")
	// TransactionSendMissingMethod a request to send a transaction was received (webhook/Kafka) that was missing method details (unexpected when using REST APIs that validate this)
	TransactionSendMissingMethod = e(100155, "Method missing - must provide inline 'param' type/value pairs with a 'methodName', or an ABI in 'method'")
	// TransactionSendBadNonce a user-supplied nonce string in the JSON input cannot be processed
	TransactionSendBadNonce = e(100156, "Converting supplied 'nonce' to integer: %s")
	// TransactionSendBadValue a user-supplied value (eth amount to transfer) string in the JSON input cannot be processed
	TransactionSendBadValue = e(100157, "Converting supplied 'value' to big integer: %s")
	// TransactionSendBadGas a user-supplied gas (maximum gas to spend on the TX) string in the JSON input cannot be processed
	TransactionSendBadGas = e(100158, "Converting supplied 'gas' to integer: %s")
	// TransactionSendBadGasPrice a user-supplied gasPrice (eth to pay for each unit of gas spent) string in the JSON input cannot be processed
	TransactionSendBadGasPrice = e(100159, "Converting supplied 'gasPrice' to big integer")
	// TransactionSendInputTypeBadNumber the input JSON value supplied for a method parameter cannot be converted to a number
	TransactionSendInputTypeBadNumber = e(100160, "Method '%s' param %s: Could not be converted to a number")
	// TransactionSendInputTypeBadJSONTypeForNumber the input JSON value supplied for a method parameter was not a number or a string, and needs to be converted to a number
	TransactionSendInputTypeBadJSONTypeForNumber = e(100161, "Method '%s' param %s is a %s: Must supply a number or a string (supplied=%s)")
	// TransactionSendInputTypeBadJSONTypeForArray the input JSON value supplied for a method parameter was not compatible with coercion to an array
	TransactionSendInputTypeBadJSONTypeForArray = e(100162, "Method '%s' param %s is a %s: Must supply an array (supplied=%s)")
	// TransactionSendInputTypeBadNull the input JSON value supplied was null
	TransactionSendInputTypeBadNull = e(100163, "Method '%s' param %s: Cannot supply a null value")
	// TransactionSendInputTypeBadJSONTypeForBoolean the input JSON value supplied for a method parameter was not compatible with coercion to a boolean
	TransactionSendInputTypeBadJSONTypeForBoolean = e(100164, "Method '%s' param %s is a %s: Must supply a boolean or a string (supplied=%s)")
	// TransactionSendInputTypeBadJSONTypeForString the input JSON value supplied for a method parameter was not compatible with coercion to a boolean
	TransactionSendInputTypeBadJSONTypeForString = e(100165, "Method '%s' param %s: Must supply a string (supplied=%s)")
	// TransactionSendInputTypeAddress the input JSON value supplied for a method parameter couldn't be parsed as an eth address
	TransactionSendInputTypeAddress = e(100166, "Method '%s' param %s: Could not be converted to a hex address (supplied=%s)")
	// TransactionSendInputTypeBadJSONTypeForAddress the input JSON value supplied for a method parameter was not compatible with coercion to an eth address
	TransactionSendInputTypeBadJSONTypeForAddress = e(100167, "Method '%s' param %s is a %s: Must supply a hex address string (supplied=%s)")
	// TransactionSendInputTypeBadJSONTypeInNumericArray one of the entries inside of a numeric array, is not valid as a number
	TransactionSendInputTypeBadJSONTypeInNumericArray = e(100168, "Method '%s' param %s is a %s: Invalid entry in number array at index %d (%s)")
	// TransactionSendInputTypeBadByteOutsideRange one of the entries inside of a byte array, is a number outside the range for bytes
	TransactionSendInputTypeBadByteOutsideRange = e(100169, "Method '%s' param %s is a %s: Invalid number - outside of range for byte")
	// TransactionSendInputTypeBadJSONTypeForBytes one of the entries inside of a byte array, is a number outside the range for bytes
	TransactionSendInputTypeBadJSONTypeForBytes = e(100170, "Method '%s' param %s is a %s: Must supply a hex string, or number array")
	// TransactionSendInputTypeBadJSONTypeForTuple if we are provided a non object input on the JSON for a struct (tuple)
	TransactionSendInputTypeBadJSONTypeForTuple = e(100171, "Method '%s' param %s is a %s: Must supply an object (supplied=%s)")
	// TransactionSendInputTypeNotSupported did not know how to handle this type - enhancement required
	TransactionSendInputTypeNotSupported = e(100172, "Type '%s' is not yet supported")
	// TransactionSendInputCountMismatch wrong number of args supplied according to the ABI
	TransactionSendInputCountMismatch = e(100173, "Method '%s': Requires %d args (supplied=%d)")
	// TransactionSendInputStructureWrong the JSON structure supplied to describe the arguments is incorrect according to our schema
	TransactionSendInputStructureWrong = e(100174, "Param %d: supplied as an object must have 'type' and 'value' fields")
	// TransactionSendInputInLineTypeArrayNotString when sending us an ABI definition for the inputs directly
	TransactionSendInputInLineTypeArrayNotString = e(100175, "Param %d: supplied as an object must be string")
	// TransactionSendInputInLineTypeUnknown when sending us an ABI definition for the inputs directly, the type string isn't known as an ethereum type
	TransactionSendInputInLineTypeUnknown = e(100176, "Param %d: Unable to map %s to etherueum type: %s")
	// TransactionSendMsgTypeUnknown we got a JSON message into the core processor (from Kafka, Webhooks etc.) that we don't understand
	TransactionSendMsgTypeUnknown = e(100177, "Unknown message type '%s'")
	// TransactionSendInputTooManyParams more parameters provided than specified on ABI
	TransactionSendInputTooManyParams = e(100178, "Supplied %d parameters for ABI that supports %d")
	// TransactionSendInputNotAssignable if we end up in a situation where the generated type cannot be assigned
	TransactionSendInputNotAssignable = e(100180, "Method %s param %s: supplied value '%+v' could not be assigned to '%s' field (%s)")

	// TransactionSendReceiptCheckError we continually had bad RCs back from the node while trying to check for the receipt up to the timeout
	TransactionSendReceiptCheckError = e(100181, "Error obtaining transaction receipt (%d retries): %s")
	// TransactionSendReceiptCheckTimeout we didn't have a problem asking the node for a receipt, but the transaction wasn't mined at the end of the timeout
	TransactionSendReceiptCheckTimeout = e(100182, "Timed out waiting for transaction receipt")

	// TransactionCallInvalidBlockNumber on "eth_call" the optional parameter for the target blocknumber failed to parse to a big integer
	TransactionCallInvalidBlockNumber = e(100183, "Invalid blocknumber. Failed to parse into big integer")

	// UnpackOutputsFailed RLP decoding of outputs, logs, or events failed
	UnpackOutputsFailed = e(100184, "Failed to unpack values: %s")
	// UnpackOutputsMismatch RLP decoding of output gave an unexpected type according to the ABI
	UnpackOutputsMismatch = e(100185, "Expected %d type in JSON/RPC response. Received %d: %+v")
	// UnpackOutputsMismatchCount wrong number of arguments
	UnpackOutputsMismatchCount = e(100186, "Expected %d in JSON/RPC response. Received %d: %+v")
	// UnpackOutputsMismatchNil RLP decoding of output gave a non-nil type, and we expected nil
	UnpackOutputsMismatchNil = e(100187, "Expected nil in JSON/RPC response. Received: %+v")
	// UnpackOutputsMismatchType expected to find a number according to supplied ABI, but got something else
	UnpackOutputsMismatchType = e(100188, "Expected %s type in JSON/RPC response for %s (%s). Received %s")
	// UnpackOutputsUnknownType did not know how to handle this type - enhancement required
	UnpackOutputsUnknownType = e(100189, "Unable to process type for %s (%s). Received %s")
	// UnpackOutputsMismatchTupleType we got a type back from the unpacking that doesn't match the ABI
	UnpackOutputsMismatchTupleType = e(100190, "Unable to process type for %s (%s). Expected %s. Received %+v")
	// UnpackOutputsMismatchTupleFieldCount we had a mismatch in the number of fields described on the ABI and the number on the go structure
	UnpackOutputsMismatchTupleFieldCount = e(100191, "Unable to process type for %s (%s). Expected %d fields on the structure. Received %d")

	// Unauthorized (401 error)
	Unauthorized = e(100192, "Unauthorized")

	// WebhooksInvalidMsgHeaders missing headers section in the JSON/YAML posted
	WebhooksInvalidMsgHeaders = e(100193, "Invalid message - missing 'headers' (or not an object)")
	// WebhooksInvalidMsgTypeMissing need to specify a msg type in the header
	WebhooksInvalidMsgTypeMissing = e(100194, "Invalid message - missing 'headers.type' (or not a string)")
	// WebhooksInvalidMsgFromMissing need to specify a msg type in the header
	WebhooksInvalidMsgFromMissing = e(100195, "Invalid message - missing 'from' (or not a string)")
	// WebhooksInvalidMsgType need to specify a valid msg type in the header
	WebhooksInvalidMsgType = e(100196, "Invalid message type: %s")
	// WebhooksKafkaUnexpectedErrFmt problem processing an error that came back from Kafka, so do a deep dump
	WebhooksKafkaUnexpectedErrFmt = e(100197, "Error did not contain message and metadata: %+v")
	// WebhooksKafkaDeliveryReportNoMeta delivery reports should contain the metadata we set when we sent
	WebhooksKafkaDeliveryReportNoMeta = e(100198, "Sent message did not contain metadata: %+v")
	// WebhooksKafkaYAMLtoJSON re-serialization of webhook message into JSON failed
	WebhooksKafkaYAMLtoJSON = e(100199, "Unable to reserialize YAML payload as JSON: %s")
	// WebhooksKafkaErr wrapper on detailed error from Kafka itself
	WebhooksKafkaErr = e(100200, "Failed to deliver message to Kafka: %s")

	// WebhooksDirectTooManyInflight when we're not using a buffered store (Kafka) we have to reject
	WebhooksDirectTooManyInflight = e(100201, "Too many in-flight transactions")
	// WebhooksDirectBadHeaders problem processing for in-memory operation
	WebhooksDirectBadHeaders = e(100202, "Failed to process headers in message")

	// LevelDBFailedRetriveOriginalKey problem retrieving entry - original key
	LevelDBFailedRetriveOriginalKey = e(100203, "Failed to retrieve the entry for the original key: %s. %s")
	// LevelDBFailedRetriveGeneratedID problem retrieving entry - generated ID
	LevelDBFailedRetriveGeneratedID = e(100204, "Failed to retrieve the entry for the generated ID: %s. %s")

	// WebSocketClosed websocket was closed
	WebSocketClosed = e(100205, "WebSocket '%s' closed")

	// CircuitBreakerTripped is returned when the Kafka circuit breaker has deemed it unsafe to produce more messages
	CircuitBreakerTripped = e(100206, "Unable to send Kafka message as the gap of %d messages between consumer and producer is too large. Estimated at %.2fKb")

	// EventSupportNotConfigured is returned when event support is not configured
	EventSupportNotConfigured = e(100207, "Event support is not configured on this gateway")

	// FFCBadVersion is returned when an FFCAPI request has a bad version header
	FFCBadVersion = e(100208, "Bad FFCAPI Version '%s': %s")
	// FFCUnsupportedVersion is returned when there is a bad version supplied on an FFCAPI request
	FFCUnsupportedVersion = e(100209, "Unsupported FFCAPI Version '%s'")
	// FFCUnsupportedRequestType is returned when the request type is unsupported for an FFCAPI request
	FFCUnsupportedRequestType = e(100210, "Unsupported FFCAPI request type '%s'")
	// FFCMissingRequestID is returned when the request ID is missing
	FFCMissingRequestID = e(100211, "Missing FFCAPI request id")
	// FFCUnmarshalABIFail is returned when failing to unmarshal a parameter to a generic go struct
	FFCUnmarshalABIFail = e(100212, "Failed to parse method ABI: %s")
	// FFCUnmarshalParamFail is returned when failing to unmarshal a parameter to a generic go struct
	FFCUnmarshalParamFail = e(100213, "Failed to parse parameter %d: %s")
	// FFCInvalidGasPrice is returned when the gas price cannot be parsed as a number (support for London fork not yet in place)
	FFCInvalidGasPrice = e(100214, "Failed to parse gasPrice '%s': %s")
	// FFCInvalidTXData is returned when the transaction input data cannot be parsed as hex
	FFCInvalidTXData = e(100215, "Failed to parse transaction data as hex '%s': %s")
	// FFCReceiptNotAvailable is returned when a receipt is not found
	FFCReceiptNotAvailable = e(100216, "Receipt not available for transaction '%s'")
	// FFCRequestTypeNotImplemented is returned when an operation is not supported
	FFCRequestTypeNotImplemented = e(100217, "FFCAPI request '%s' not currently supported")
	// FFCBlockNotAvailable is returned when a receipt is not found
	FFCBlockNotAvailable = e(100218, "Block not available")

	// ReceiptStoreKeyNotUnique non-unique request ID
	ReceiptStoreKeyNotUnique = e(100219, "Request ID is not unique")
	// ReceiptErrorIdempotencyCheck failed to query receipt during idempotency check
	ReceiptErrorIdempotencyCheck = e(100220, "Failed querying the receipt store, performing duplicate message check on ackmode=receipt for id %s: %s")
	// ResubmissionPreventedCheckTransactionHash redelivery was prevented by the processor
	ResubmissionPreventedCheckTransactionHash = e(100221, "Resubmission of this transaction was prevented by the REST API Gateway. Check the status of the transaction by the transaction hash")
)

type EthconnectError interface {
	Code() string
	Error() string
	ErrorNoCode() string
	String() string
}

type ethconnectError struct {
	msg     *errorID
	inserts []interface{}
}

func (e *ethconnectError) ErrorNoCode() string {
	return fmt.Sprintf(e.msg.enMsg, e.inserts...)
}

func (e *ethconnectError) Error() string {
	return fmt.Sprintf("%s: %s", e.msg.Code(), e.ErrorNoCode())
}

func (e *ethconnectError) Code() string {
	return e.msg.Code()
}

func (e *ethconnectError) String() string {
	return e.Error()
}

type RESTError struct {
	Message string `json:"error"`
	Code    string `json:"code,omitempty"`
}

func ToRESTError(err error) *RESTError {
	var errorMessage string
	var errorCode = ""
	switch err := err.(type) {
	case EthconnectError:
		errorMessage = err.ErrorNoCode()
		errorCode = err.Code()
	default:
		errorMessage = err.Error()
	}
	return &RESTError{Message: errorMessage, Code: errorCode}
}

// Errorf creates an error (not yet translated, but an extensible interface for that using simple sprintf formatting rather than named i18n inserts)
func Errorf(msg ErrorID, inserts ...interface{}) EthconnectError {
	return &ethconnectError{msg.(*errorID), inserts}
}
