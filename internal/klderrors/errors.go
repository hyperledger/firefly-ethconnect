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

package klderrors

import (
	"fmt"

	"github.com/pkg/errors"
)

// ErrorID enumerates all errors in ethconnect.
type ErrorID string

const (

	// AddressBookLookupBadURL we got back a bad URL from the remote address book after our REST call
	AddressBookLookupBadURL = "Invalid URL obtained for address"
	// AddressBookLookupBadHostsFile we have a custom hosts file for DNS resolution, but it cannot be processed
	AddressBookLookupBadHostsFile = "Configuration problem (hosts file)"
	// AddressBookLookupNotFound remote addressbook says no
	AddressBookLookupNotFound = "Unknown address"

	// ConfigFileReadFailed failed to read the server config file
	ConfigFileReadFailed = "Failed to read %s: %s"
	// CompilerVersionNotFound the runtime context of ethconnect has not been configured with a compiler for the requested version
	CompilerVersionNotFound = "Could not find a configured compiler for requested Solidity major version %s.%s"
	// CompilerVersionBadRequest the user requested a bad semver
	CompilerVersionBadRequest = "Invalid Solidity version requested for compiler. Ensure the string starts with two dot separated numbers, such as 0.5"
	// CompilerFailedSolc compilation failure output from solc
	CompilerFailedSolc = "Solidity compilation failed: solc: %v\n%s"
	// CompilerOutputMissingContract the output from the compiler does not include the requested contract
	CompilerOutputMissingContract = "Contract '%s' not found in Solidity source: %s"
	// CompilerOutputMultipleContracts need to select one
	CompilerOutputMultipleContracts = "More than one contract in Solidity file, please set one to call: %s"
	// CompilerBytecodeInvalid hex output from compiler could not be parsed
	CompilerBytecodeInvalid = "Decoding bytecode: %s"
	// CompilerBytecodeEmpty null result from succcessful compile in solc
	CompilerBytecodeEmpty = "Specified contract compiled ok, but did not result in any bytecode: %s"
	// CompilerABISerialize could not serialize the ABI output from solc
	CompilerABISerialize = "Serializing ABI: %s"
	// CompilerABIReRead could not re-read serialized output after writing the ABI
	CompilerABIReRead = "Parsing ABI: %s"
	// CompilerSerializeDevDocs could not serialize the dev docs output from solc
	CompilerSerializeDevDocs = "Serializing DevDoc: %s"
	// ConfigNoRPC missing config for JSON/RPC
	ConfigNoRPC = "No JSON/RPC URL set for ethereum node"
	// ConfigKafkaMissingOutputTopic response topic missing
	ConfigKafkaMissingOutputTopic = "No output topic specified for bridge to send events to"
	// ConfigKafkaMissingInputTopic request topic missing
	ConfigKafkaMissingInputTopic = "No input topic specified for bridge to listen to"
	// ConfigKafkaMissingConsumerGroup consumer group missing
	ConfigKafkaMissingConsumerGroup = "No consumer group specified"
	// ConfigKafkaMissingBadSASL problem with SASL config
	ConfigKafkaMissingBadSASL = "Username and Password must both be provided for SASL"
	// ConfigKafkaMissingBrokers missing/empty brokers
	ConfigKafkaMissingBrokers = "No Kafka brokers configured"
	// ConfigRESTGatewayRequiredReceiptStore need to enable params for REST Gatewya
	ConfigRESTGatewayRequiredReceiptStore = "MongoDB URL, Database and Collection name must be specified to enable the receipt store"
	// ConfigRESTGatewayRequiredRPC and RPC stuff
	ConfigRESTGatewayRequiredRPC = "RPC URL and Storage Path must be supplied to enable the Open API REST Gateway"
	// ConfigWebhooksDirectRPC for webhooks direct
	ConfigWebhooksDirectRPC = "No JSON/RPC URL set for ethereum node"
	// ConfigTLSCertOrKey incomplete TLS config
	ConfigTLSCertOrKey = "Client private key and certificate must both be provided for mutual auth"

	// ConfigNoYAML missing configuration file on server start
	ConfigNoYAML = "No YAML configuration filename specified"
	// ConfigYAMLParseFile failed to parse YAML during server startup
	ConfigYAMLParseFile = "Unable to parse %s as YAML: %s"
	// ConfigYAMLPostParseFile failed to process YAML as JSON after parsing
	ConfigYAMLPostParseFile = "Failed to process YAML config from %s: %s"

	// DeployTransactionMissingCode a DeployTransaction message, without code to deploy
	DeployTransactionMissingCode = "Missing Compiled Code + ABI, or Solidity"

	// EventStreamsDBLoad failed to init DB
	EventStreamsDBLoad = "Failed to open DB at %s: %s"
	// EventStreamsNoID attempt to create an event stream/sub without an ID
	EventStreamsNoID = "No ID"
	// EventStreamsInvalidActionType unknown action type
	EventStreamsInvalidActionType = "Unknown action type '%s'"
	// EventStreamsWebhookNoURL attempt to create a Webhook event stream without a URL
	EventStreamsWebhookNoURL = "Must specify webhook.url for action type 'webhook'"
	// EventStreamsWebhookInvalidURL attempt to create a Webhook event stream with an invalid URL
	EventStreamsWebhookInvalidURL = "Invalid URL in webhook action"
	// EventStreamsWebhookResumeActive resume when already resumed
	EventStreamsWebhookResumeActive = "Event processor is already active. Suspending:%t"
	// EventStreamsWebhookProhibitedAddress some IP ranges can be restricted
	EventStreamsWebhookProhibitedAddress = "Cannot send Webhook POST to address: %s"
	// EventStreamsWebhookFailedHTTPStatus server at the other end of a webhook returned a non-OK response
	EventStreamsWebhookFailedHTTPStatus = "%s: Failed with status=%d"
	// EventStreamsSubscribeBadBlock the starting block for a subscription request is invalid
	EventStreamsSubscribeBadBlock = "FromBlock cannot be parsed as a BigInt"
	// EventStreamsSubscribeStoreFailed problem saving a subscription to our DB
	EventStreamsSubscribeStoreFailed = "Failed to store subscription: %s"
	// EventStreamsSubscribeNoEvent missing event
	EventStreamsSubscribeNoEvent = "Solidity event name must be specified"
	// EventStreamsSubscriptionNotFound sub not found
	EventStreamsSubscriptionNotFound = "Subscription with ID '%s' not found"
	// EventStreamsCreateStreamStoreFailed problem saving a subscription to our DB
	EventStreamsCreateStreamStoreFailed = "Failed to store stream: %s"
	// EventStreamsStreamNotFound stream not found
	EventStreamsStreamNotFound = "Stream with ID '%s' not found"
	// EventStreamsLogDecode problem decoding the logs for an event emitted on the chain
	EventStreamsLogDecode = "%s: Failed to decode data: %s"
	// EventStreamsLogDecodeInsufficientTopics ran out of topics according to the indexed fields described on the ABI event
	EventStreamsLogDecodeInsufficientTopics = "%s: Ran out of topics for indexed fields at field %d of %+v"
	// EventStreamsLogDecodeData RLP decoding of the data section of the logs failed
	EventStreamsLogDecodeData = "%s: Failed to parse RLP data from event: %s"

	// KakfaProducerConfirmMsgUnknown we received a confirmation callback, but we aren't expecting it
	KakfaProducerConfirmMsgUnknown = "Received confirmation for message not in in-flight map: %s"

	// KVStoreDBLoad failed to init DB
	KVStoreDBLoad = "Failed to open DB at %s: %s"
	// KVStoreMemFilteringUnsupported memory db is really just for testing. No filtering support
	KVStoreMemFilteringUnsupported = "Memory receipts do not support filtering"

	// HDWalletSigningFailed problem returned from remote HDWallet API
	HDWalletSigningFailed = "HDWallet signing failed"
	// HDWalletSigningBadData we got a response, but not with the correct fields
	HDWalletSigningBadData = "Unexpected response from HDWallet"
	// HDWalletSigningNoConfig we had a request for HD Wallet signing, but we don't have the required config
	HDWalletSigningNoConfig = "No HD Wallet Configuration"

	// HelperStrToAddressRequiredField re-usable error for missing fields
	HelperStrToAddressRequiredField = "'%s' must be supplied"
	// HelperStrToAddressBadAddress re-usable error for bad address
	HelperStrToAddressBadAddress = "Supplied value for '%s' is not a valid hex address"
	// HelperYAMLorJSONPayloadTooLarge input message too large
	HelperYAMLorJSONPayloadTooLarge = "Message exceeds maximum allowable size"
	// HelperYAMLorJSONPayloadReadFailed failed to read input
	HelperYAMLorJSONPayloadReadFailed = "Unable to read input data: %s"
	// HelperYAMLorJSONPayloadParseFailed input message got error parsing
	HelperYAMLorJSONPayloadParseFailed = "Unable to parse as YAML or JSON: %s"

	// HTTPRequesterSerializeFailed common HTTP request utility for extensions, failed to serialize request
	HTTPRequesterSerializeFailed = "Failed to serialize request payload: %s"
	// HTTPRequesterNonStatusError common HTTP request utility for extensions, got an error sending a request
	HTTPRequesterNonStatusError = "Error querying %s"
	// HTTPRequesterStatusErrorNoData common HTTP request utility for extensions, got a status code, but couldn't deserialize payload
	HTTPRequesterStatusErrorNoData = "Could not process %s [%d] response"
	// HTTPRequesterStatusErrorWithData common HTTP request utility for extensions, got a non-ok status code with JSON errorMessage
	HTTPRequesterStatusErrorWithData = "%s returned [%d]: %s"
	// HTTPRequesterStatusError common HTTP request utility for extensions, got a non-ok status code
	HTTPRequesterStatusError = "Error querying %s"
	// HTTPRequesterResponseMissingField common HTTP request utility for extensions, missing expected field in response
	HTTPRequesterResponseMissingField = "'%s' missing in %s response"
	// HTTPRequesterResponseNonStringField common HTTP request utility for extensions, expected string for field in response
	HTTPRequesterResponseNonStringField = "'%s' not a string in %s response"
	// HTTPRequesterResponseNullField common HTTP request utility for extensions, expected non-empty response field
	HTTPRequesterResponseNullField = "'%s' empty (or null) in %s response"

	// ReceiptStoreDisabled not configured
	ReceiptStoreDisabled = "Receipt store not enabled"
	// ReceiptStoreDBLoad failed to init DB
	ReceiptStoreDBLoad = "Failed to open DB at %s: %s"
	// ReceiptStoreMongoDBConnect couldn't connect to MongoDB
	ReceiptStoreMongoDBConnect = "Unable to connect to MongoDB: %s"
	// ReceiptStoreMongoDBIndex couldn't create MongoDB index
	ReceiptStoreMongoDBIndex = "Unable to create index: %s"
	// ReceiptStoreSerializeResponse problem sending a receipt stored back over the REST API
	ReceiptStoreSerializeResponse = "Error serializing response"
	// ReceiptStoreInvalidRequestID bad ID query
	ReceiptStoreInvalidRequestID = "Invalid 'id' query parameter"
	// ReceiptStoreInvalidRequestMaxLimit bad limit over max
	ReceiptStoreInvalidRequestMaxLimit = "Maximum limit is %d"
	// ReceiptStoreInvalidRequestBadLimit bad limit
	ReceiptStoreInvalidRequestBadLimit = "Invalid 'limit' query parameter"
	// ReceiptStoreInvalidRequestBadSkip bad skip
	ReceiptStoreInvalidRequestBadSkip = "Invalid 'skip' query parameter"
	// ReceiptStoreInvalidRequestBadSince bad since
	ReceiptStoreInvalidRequestBadSince = "since cannot be parsed as RFC3339 or millisecond timestamp"
	// ReceiptStoreFailedQuery wrapper over detailed error
	ReceiptStoreFailedQuery = "Error querying replies: %s"
	// ReceiptStoreFailedQuerySingle wrapper over detailed error
	ReceiptStoreFailedQuerySingle = "Error querying reply: %s"
	// ReceiptStoreFailedNotFound receipt isn't in the store
	ReceiptStoreFailedNotFound = "Receipt not available"

	// RemoteRegistryCacheInit initialzation issue for remote contract registry
	RemoteRegistryCacheInit = "Failed to initialize cache for remote registry: %s"
	// RemoteRegistryNotConfigured cannot register as a remote registry is not configured
	RemoteRegistryNotConfigured = "No remote registry is configured"
	// RemoteRegistryRegistrationFailed error during registration with remote contract registry
	RemoteRegistryRegistrationFailed = "Failed to register instance in remote registry: %s"
	// RemoteRegistryLookupGatewayNotFound did not find the requested ID in the remote registry for a gateway/factory
	RemoteRegistryLookupGatewayNotFound = "Gateway not found"
	// RemoteRegistryLookupInstanceNotFound did not find the requested ID in the remote registry for a contract instance
	RemoteRegistryLookupInstanceNotFound = "Instance not found"
	// RemoteRegistryLookupGenericProcessingFailed we don't return the full original error over the REST API after logging
	RemoteRegistryLookupGenericProcessingFailed = "Error processing contract registry response"

	// RESTGatewayGatewayNotFound the gateway REST API interface (the 'factory' / ABI generic interface) was not found
	RESTGatewayGatewayNotFound = "Gateway not found"
	// RESTGatewayInstanceNotFound the instance REST API interface (an individual registered address) was not found
	RESTGatewayInstanceNotFound = "Instance not found"
	// RESTGatewayEventNotDeclared attempt to subscribe to an event on an instance that does not exist
	RESTGatewayEventNotDeclared = "Event '%s' is not declared in the ABI"
	// RESTGatewayMethodNotDeclared attempt to invoke a method name that does not exist in the ABI, or register globally for an event that doesn't exist
	RESTGatewayMethodNotDeclared = "Method or Event '%s' is not declared in the ABI of contract '%s'"
	// RESTGatewayInvalidToAddress failed to parse a 'to' address supplied on a path
	RESTGatewayInvalidToAddress = "To Address must be a 40 character hex string (0x prefix is optional)"
	// RESTGatewayInvalidFromAddress failed to parse a 'from' address supplied on a path
	RESTGatewayInvalidFromAddress = "From Address must be a 40 character hex string (0x prefix is optional)"
	// RESTGatewayMissingParameter did not supply a parameter required by the method
	RESTGatewayMissingParameter = "Parameter '%s' of method '%s' was not specified in body or query parameters"
	// RESTGatewayMissingFromAddress did not supply a signing address for the transaction
	RESTGatewayMissingFromAddress = "Please specify a valid address in the 'kld-from' query string parameter or x-kaleido-from HTTP header"
	// RESTGatewaySubscribeMissingStreamParameter missed the ID of the stream when registering
	RESTGatewaySubscribeMissingStreamParameter = "Must supply a 'stream' parameter in the body or query"
	// RESTGatewayMixedPrivateForAndGroupID confused privacy group info, using simple/Tessera style as well as pre-defined/Orion style
	RESTGatewayMixedPrivateForAndGroupID = "kld-privatefor and kld-privacygroupid are mutually exclusive"
	// RESTGatewayEventManagerInitFailed constructor failure for event manager
	RESTGatewayEventManagerInitFailed = "Event-stream subscription manager: %s"
	// RESTGatewayEventStreamInvalid attempt to create an event stream with invalid parameters
	RESTGatewayEventStreamInvalid = "Invalid event stream specification: %s"
	// RESTGatewayPostDeployMissingAddress after deployment the receipt did not contain a contract address
	RESTGatewayPostDeployMissingAddress = "%s: Missing contract address in receipt"
	// RESTGatewayRegistrationSuppliedInvalidAddress invalid address when registering an existing instance of a contract
	RESTGatewayRegistrationSuppliedInvalidAddress = "Invalid address in path - must be a 40 character hex string with optional 0x prefix"
	// RESTGatewaySyncMsgTypeMismatch sync-invoke code paths in REST API Gateway should be maintained such that this cannot happen
	RESTGatewaySyncMsgTypeMismatch = "Unexpected condition (message types do not match when processing)"
	// RESTGatewaySyncWrapErrorWithTXDetail wraps a low level error with transaction hash context on sync APIs before returning
	RESTGatewaySyncWrapErrorWithTXDetail = "TX %s: %s"

	// RESTGatewayCompileContractInvalidFormData invalid form data when requesting a compilation to generate an ABI/bytecode
	RESTGatewayCompileContractInvalidFormData = "Could not parse supplied multi-part form data: %s"
	// RESTGatewayCompileContractCompileFailed failed to perform compile
	RESTGatewayCompileContractCompileFailed = "Failed to compile solidity: %s"
	// RESTGatewayCompileContractPostCompileFailed failed to process output of compilation
	RESTGatewayCompileContractPostCompileFailed = "Failed to process solidity: %s"
	// RESTGatewayCompileContractExtractedReadFailed failed to read extracted contents of uploaded data
	RESTGatewayCompileContractExtractedReadFailed = "Failed to read extracted multi-part form data"
	// RESTGatewayCompileContractNoSOL failed to find any solidity files in uploaded data
	RESTGatewayCompileContractNoSOL = "No .sol files found in root. Please set a 'source' query param or form field to the relative path of your solidity"
	// RESTGatewayCompileContractSolcVerFail failed while checking version of solidity compiler 'solc'
	RESTGatewayCompileContractSolcVerFail = "Failed checking solc version: %s"
	// RESTGatewayCompileContractCompileFailDetails output from compiler failure
	RESTGatewayCompileContractCompileFailDetails = "Failed to compile [%s]: %s"
	// RESTGatewayCompileContractSolcOutputProcessFail failed to process output of compilation
	RESTGatewayCompileContractSolcOutputProcessFail = "Failed to parse solc output: %s"
	// RESTGatewayCompileContractSlashes unsafe slash characters in filenames
	RESTGatewayCompileContractSlashes = "Filenames cannot contain slashes. Use a zip file to upload a directory structure"
	// RESTGatewayCompileContractUnzipRead error opening zip/tgz to read (no extra information to remote caller)
	RESTGatewayCompileContractUnzipRead = "Failed to read archive"
	// RESTGatewayCompileContractUnzipWrite error writing extracted zip (no extra information to remote caller)
	RESTGatewayCompileContractUnzipWrite = "Failed to process archive"
	// RESTGatewayCompileContractUnzipCopy error writing extracted zip (no extra information to remote caller)
	RESTGatewayCompileContractUnzipCopy = "Failed to process archive"
	// RESTGatewayCompileContractUnzip failure thrown from decompression library during extract
	RESTGatewayCompileContractUnzip = "Error unarchiving supplied zip file: %s"

	// RESTGatewayLocalStoreContractSave local filesystem storage failure for contract instance (non-registry code flow)
	RESTGatewayLocalStoreContractSave = "Failed to write ABI JSON: %s"
	// RESTGatewayLocalStoreContractLoad local filesystem load failure for contract instance (non-registry code flow)
	RESTGatewayLocalStoreContractLoad = "Failed to find installed contract address for '%s'"
	// RESTGatewayLocalStoreContractNotFound local filesystem not found (non-registry code flow)
	RESTGatewayLocalStoreContractNotFound = "No contract instance registered with address %s"
	// RESTGatewayLocalStoreABINotFound lookup of ABI failed not found (non-registry code flow)
	RESTGatewayLocalStoreABINotFound = "No ABI found with ID %s"
	// RESTGatewayLocalStoreABILoad local filesystem load failure for ABI details (non-registry code flow)
	RESTGatewayLocalStoreABILoad = "Failed to load ABI with ID %s: %s"
	// RESTGatewayLocalStoreABIParse local filesystem parse failure for ABI details (non-registry code flow)
	RESTGatewayLocalStoreABIParse = "Failed to parse ABI with ID %s: %s"
	// RESTGatewayLocalStoreMissingABI did not supply ABI JSON when attempting to install ABI (non-registry code flow)
	RESTGatewayLocalStoreMissingABI = "Must supply ABI to install an existing ABI into the REST Gateway"
	// RESTGatewayLocalStoreContractSavePostDeploy local filesystem storage failure for contract instance post deploy (non-registry code flow)
	RESTGatewayLocalStoreContractSavePostDeploy = "%s: Failed to write deployment details: %s"
	// RESTGatewayFriendlyNameClash duplicate friendly name when reigstering
	RESTGatewayFriendlyNameClash = "Contract address %s is already registered for name '%s'"

	// RPCCallReturnedError specified RPC call returned error
	RPCCallReturnedError = "%s returned: %s"
	// RPCConnectFailed error connecting to back-end server over JSON/RPC
	RPCConnectFailed = "JSON/RPC connection to %s failed: %s"

	// SecurityModulePluginLoad failed to load .so
	SecurityModulePluginLoad = "Failed to load plugin: %s"
	// SecurityModulePluginSymbol missing symbol in plugin
	SecurityModulePluginSymbol = "Failed to load 'SecurityModule' symbol from '%s': %s"
	// SecurityModuleNoAuthContext missing auth context in context object at point security module is invoked
	SecurityModuleNoAuthContext = "No auth context"

	// TransactionSendConstructorPackArgs RLP encoding failure for a constructor
	TransactionSendConstructorPackArgs = "Packing arguments for constructor: %s"
	// TransactionSendMethodPackArgs RLP encoding failure for a method
	TransactionSendMethodPackArgs = "Packing arguments for method '%s': %s"
	// TransactionSendInputTypeUnknown there is a type in the ABI inputs that we don't understand
	TransactionSendInputTypeUnknown = "ABI input %d: Unable to map %s to etherueum type: %s"
	// TransactionSendOutputTypeUnknown there is a type in the ABI outputs that we don't understand
	TransactionSendOutputTypeUnknown = "ABI output %d: Unable to map %s to etherueum type: %s"
	// TransactionSendGasEstimateFailed gas estimation failed prior to sending TX
	TransactionSendGasEstimateFailed = "Failed to calculate gas for transaction: %s"
	// TransactionSendCallFailedNoRevert failed to perform an eth_call with a JSON/RPC error (not a revert)
	TransactionSendCallFailedNoRevert = "Call failed: %s"
	// TransactionSendCallFailedRevertMessage directly passes the revert message from the EVM
	TransactionSendCallFailedRevertMessage = "%s"
	// TransactionSendCallFailedRevertNoMessage when we couldn't process the EVM revert message
	TransactionSendCallFailedRevertNoMessage = "EVM reverted. Failed to decode error message"
	// TransactionSendMissingPrivateFromOrion there is no default privateFrom in Orion, so the user must always supply it
	TransactionSendMissingPrivateFromOrion = "private-from is required when submitting private transactions via Orion"
	// TransactionSendPrivateTXWithExternalSigner we don't allow private transactions to be combined with a HD Wallet or other external signer currently
	TransactionSendPrivateTXWithExternalSigner = "Signing with %s is not currently supported with private transactions"
	// TransactionSendPrivateForAndPrivacyGroup mixed both params
	TransactionSendPrivateForAndPrivacyGroup = "privacyGroupId and privateFor are mutually exclusive"
	// TransactionSendNonceFailWithPrivacyGroup when we successfully lookup the privacy group, but cannot get the nonce
	TransactionSendNonceFailWithPrivacyGroup = "priv_getTransactionCount for privacy group '%s' returned: %s"
	// TransactionSendMissingMethod a request to send a transaction was received (webhook/Kafka) that was missing method details (unexpected when using REST APIs that validate this)
	TransactionSendMissingMethod = "Method missing - must provide inline 'param' type/value pairs with a 'methodName', or an ABI in 'method'"
	// TransactionSendBadNonce a user-supplied nonce string in the JSON input cannot be processed
	TransactionSendBadNonce = "Converting supplied 'nonce' to integer: %s"
	// TransactionSendBadValue a user-supplied value (eth amount to transfer) string in the JSON input cannot be processed
	TransactionSendBadValue = "Converting supplied 'value' to big integer: %s"
	// TransactionSendBadGas a user-supplied gas (maximum gas to spend on the TX) string in the JSON input cannot be processed
	TransactionSendBadGas = "Converting supplied 'gas' to integer: %s"
	// TransactionSendBadGasPrice a user-supplied gasPrice (eth to pay for each unit of gas spent) string in the JSON input cannot be processed
	TransactionSendBadGasPrice = "Converting supplied 'gasPrice' to big integer"
	// TransactionSendInputTypeBadNumber the input JSON value supplied for a method parameter cannot be converted to a number
	TransactionSendInputTypeBadNumber = "Method '%s' param %d: Could not be converted to a number"
	// TransactionSendInputTypeBadJSONTypeForNumber the input JSON value supplied for a method parameter was not a number or a string, and needs to be converted to a number
	TransactionSendInputTypeBadJSONTypeForNumber = "Method '%s' param %d is a %s: Must supply a number or a string"
	// TransactionSendInputTypeBadJSONTypeForArray the input JSON value supplied for a method parameter was not compatible with coercion to an array
	TransactionSendInputTypeBadJSONTypeForArray = "Method '%s' param %d is a %s: Must supply an array"
	// TransactionSendInputTypeBadNull the input JSON value supplied was null
	TransactionSendInputTypeBadNull = "Method '%s' param %d: Cannot supply a null value"
	// TransactionSendInputTypeBadJSONTypeForBoolean the input JSON value supplied for a method parameter was not compatible with coercion to a boolean
	TransactionSendInputTypeBadJSONTypeForBoolean = "Method '%s' param %d is a %s: Must supply a boolean or a string"
	// TransactionSendInputTypeBadJSONTypeForString the input JSON value supplied for a method parameter was not compatible with coercion to a boolean
	TransactionSendInputTypeBadJSONTypeForString = "Method '%s' param %d: Must supply a string"
	// TransactionSendInputTypeAddress the input JSON value supplied for a method parameter couldn't be parsed as an eth address
	TransactionSendInputTypeAddress = "Method '%s' param %d: Could not be converted to a hex address"
	// TransactionSendInputTypeBadJSONTypeForAddress the input JSON value supplied for a method parameter was not compatible with coercion to an eth address
	TransactionSendInputTypeBadJSONTypeForAddress = "Method '%s' param %d is a %s: Must supply a hex address string"
	// TransactionSendInputTypeBadJSONTypeInNumericArray one of the entries inside of a numeric array, is not valid as a number
	TransactionSendInputTypeBadJSONTypeInNumericArray = "Method '%s' param %d is a %s: Invalid entry in number array at index %d (%s)"
	// TransactionSendInputTypeBadByteOutsideRange one of the entries inside of a byte array, is a number outside the range for bytes
	TransactionSendInputTypeBadByteOutsideRange = "Method '%s' param %d is a %s: Invalid number - outside of range for byte"
	// TransactionSendInputTypeBadJSONTypeForBytes one of the entries inside of a byte array, is a number outside the range for bytes
	TransactionSendInputTypeBadJSONTypeForBytes = "Method '%s' param %d is a %s: Must supply a hex string, or number array"
	// TransactionSendInputTypeNotSupported did not know how to handle this type - enhancement required
	TransactionSendInputTypeNotSupported = "Type '%s' is not yet supported"
	// TransactionSendInputCountMismatch wrong number of args supplied according to the ABI
	TransactionSendInputCountMismatch = "Method '%s': Requires %d args (supplied=%d)"
	// TransactionSendInputStructureWrong the JSON structure supplied to decribe the arguments is incorrect according to our schema
	TransactionSendInputStructureWrong = "Param %d: supplied as an object must have 'type' and 'value' fields"
	// TransactionSendInputInLineTypeArrayNotString when sending us an ABI definition for the inputs directly
	TransactionSendInputInLineTypeArrayNotString = "Param %d: supplied as an object must be string"
	// TransactionSendInputInLineTypeUnknown when sending us an ABI definition for the inputs directly, the type string isn't known as an ethereum type
	TransactionSendInputInLineTypeUnknown = "Param %d: Unable to map %s to etherueum type: %s"
	// TransactionSendMsgTypeUnknown we got a JSON message into the core processor (from Kafka, Webhooks etc.) that we don't understand
	TransactionSendMsgTypeUnknown = "Unknown message type '%s'"

	// TransactionSendReceiptCheckError we continually had bad RCs back from the node while trying to check for the receipt up to the timeout
	TransactionSendReceiptCheckError = "Error obtaining transaction receipt (%d retries): %s"
	// TransactionSendReceiptCheckTimeout we didn't have a problem asking the node for a receipt, but the transaction wasn't mined at the end of the timeout
	TransactionSendReceiptCheckTimeout = "Timed out waiting for transaction receipt"

	// TransactionCallInvalidBlockNumber on "eth_call" the optional parameter for the target blocknumber failed to parse to a big integer
	TransactionCallInvalidBlockNumber = "Invalid blocknumber. Failed to parse into big integer"

	// UnpackOutputsFailed RLP decoding of outputs, logs, or events failed
	UnpackOutputsFailed = "Failed to unpack values: %s"
	// UnpackOutputsMismatch RLP decoding of output gave an unexpected type according to the ABI
	UnpackOutputsMismatch = "Expected %d type in JSON/RPC response. Received %d: %+v"
	// UnpackOutputsMismatchCount wrong number of arguments
	UnpackOutputsMismatchCount = "Expected %d in JSON/RPC response. Received %d: %+v"
	// UnpackOutputsMismatchNil RLP decoding of output gave a non-nil type, and we expected nil
	UnpackOutputsMismatchNil = "Expected nil in JSON/RPC response. Received: %+v"
	// UnpackOutputsMismatchType expected to find a number according to supplied ABI, but got something else
	UnpackOutputsMismatchType = "Expected %s type in JSON/RPC response for %s (%s). Received %s"
	// UnpackOutputsUnknownType did not know how to handle this type - enhancement required
	UnpackOutputsUnknownType = "Unable to process type for %s (%s). Received %s"

	// Unauthorized (401 error)
	Unauthorized = "Unauthorized"

	// WebhooksInvalidMsgHeaders missing headers section in the JSON/YAML posted
	WebhooksInvalidMsgHeaders = "Invalid message - missing 'headers' (or not an object)"
	// WebhooksInvalidMsgTypeMissing need to specify a msg type in the header
	WebhooksInvalidMsgTypeMissing = "Invalid message - missing 'headers.type' (or not a string)"
	// WebhooksInvalidMsgFromMissing need to specify a msg type in the header
	WebhooksInvalidMsgFromMissing = "Invalid message - missing 'from' (or not a string)"
	// WebhooksInvalidMsgType need to specify a valid msg type in the header
	WebhooksInvalidMsgType = "Invalid message type: %s"
	// WebhooksKafkaUnexpectedErrFmt problem processing an error that came back from Kafka, so do a deep dump
	WebhooksKafkaUnexpectedErrFmt = "Error did not contain message and metadata: %+v"
	// WebhooksKafkaDeliveryReportNoMeta delivery reports should contain the metadata we set when we sent
	WebhooksKafkaDeliveryReportNoMeta = "Sent message did not contain metadata: %+v"
	// WebhooksKafkaYAMLtoJSON re-serialization of webhook message into JSON failed
	WebhooksKafkaYAMLtoJSON = "Unable to reserialize YAML payload as JSON: %s"
	// WebhooksKafkaErr wrapper on detailed error from Kafka itself
	WebhooksKafkaErr = "Failed to deliver message to Kafka: %s"

	// WebhooksDirectTooManyInflight when we're not using a buffered store (Kafka) we have to reject
	WebhooksDirectTooManyInflight = "Too many in-flight transactions"
	// WebhooksDirectBadHeaders problem processing for in-memory operation
	WebhooksDirectBadHeaders = "Failed to process headers in message"
)

type kldError string

func (e kldError) Error() string {
	return string(e)
}

// Errorf creates an error (not yet translated, but an extensible interface for that using simple sprintf formatting rather than named i18n inserts)
func Errorf(msg ErrorID, inserts ...interface{}) error {
	var err error = kldError(fmt.Sprintf(string(msg), inserts...))
	return errors.WithStack(err)
}
