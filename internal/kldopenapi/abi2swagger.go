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

package kldopenapi

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/go-openapi/jsonreference"
	"github.com/go-openapi/spec"
	"github.com/tidwall/gjson"
)

// ABI2Swagger is the main entry point for conversion
type ABI2Swagger struct {
	externalHost     string
	externalSchemes  []string
	externalRootPath string
	orionPrivateAPI  bool
}

const (
	kaleidoAppCredential = "KaleidoAppCredential"
)

// NewABI2Swagger constructor
func NewABI2Swagger(externalHost, externalRootPath string, externalSchemes []string, orionPrivateAPI bool) *ABI2Swagger {
	c := &ABI2Swagger{
		externalHost:     externalHost,
		externalRootPath: externalRootPath,
		externalSchemes:  externalSchemes,
		orionPrivateAPI:  orionPrivateAPI,
	}
	if len(c.externalSchemes) == 0 {
		c.externalSchemes = []string{"http", "https"}
	}
	return c
}

// Gen4Instance generates OpenAPI for a single contract instance with an address
func (c *ABI2Swagger) Gen4Instance(basePath, name string, abi *abi.ABI, devdocsJSON string) *spec.Swagger {
	return c.convert(basePath, name, abi, devdocsJSON, true, false, false)
}

// Gen4Factory generates OpenAPI for a contract factory, with a constructor, and child methods on any addres
func (c *ABI2Swagger) Gen4Factory(basePath, name string, factoryOnly, externalRegistry bool, abi *abi.ABI, devdocsJSON string) *spec.Swagger {
	return c.convert(basePath, name, abi, devdocsJSON, false, factoryOnly, externalRegistry)
}

// convert does the conversion and fills in the details on the Swagger Schema
func (c *ABI2Swagger) convert(basePath, name string, abi *abi.ABI, devdocsJSON string, inst, factoryOnly, externalRegistry bool) *spec.Swagger {

	basePath = c.externalRootPath + basePath

	devdocs := gjson.Parse(devdocsJSON)

	paths := &spec.Paths{}
	paths.Paths = make(map[string]spec.PathItem)
	definitions := make(map[string]spec.Schema)
	parameters := c.getCommonParameters()
	c.buildDefinitionsAndPaths(inst, factoryOnly, externalRegistry, abi, definitions, paths.Paths, devdocs)
	return &spec.Swagger{
		SwaggerProps: spec.SwaggerProps{
			Swagger: "2.0",
			Info: &spec.Info{
				InfoProps: spec.InfoProps{
					Version:     "1.0",
					Title:       name,
					Description: devdocs.Get("details").String(),
				},
			},
			Host:        c.externalHost,
			Schemes:     c.externalSchemes,
			BasePath:    basePath,
			Paths:       paths,
			Definitions: definitions,
			Parameters:  parameters,
			SecurityDefinitions: map[string]*spec.SecurityScheme{
				kaleidoAppCredential: &spec.SecurityScheme{
					SecuritySchemeProps: spec.SecuritySchemeProps{
						Type: "basic",
					},
				},
			},
		},
	}
}

func (c *ABI2Swagger) buildDefinitionsAndPaths(inst, factoryOnly, externalRegistry bool, abi *abi.ABI, defs map[string]spec.Schema, paths map[string]spec.PathItem, devdocs gjson.Result) {
	methodsDocs := devdocs.Get("methods")
	if !inst {
		c.buildMethodDefinitionsAndPath(inst, defs, paths, "constructor", abi.Constructor, methodsDocs)
	}
	if !factoryOnly {
		if !inst && !externalRegistry {
			c.addRegisterPath(paths)
		}
		for _, method := range abi.Methods {
			c.buildMethodDefinitionsAndPath(inst, defs, paths, method.Name, method, methodsDocs)
		}
		for _, event := range abi.Events {
			c.buildEventDefinitionsAndPath(inst, defs, paths, event.Name, event, devdocs.Get("events"))
			if !inst {
				// We add the event again at the top level (as if it were an instance) on non-instance
				// swagger definitions, so you can subscribe to all events of this type on all instances
				c.buildEventDefinitionsAndPath(true, defs, paths, event.Name, event, devdocs.Get("events"))
			}
		}
	}
	errSchema := spec.Schema{
		SchemaProps: spec.SchemaProps{
			Properties: make(map[string]spec.Schema),
		},
	}
	errSchema.Properties["error"] = spec.Schema{
		SchemaProps: spec.SchemaProps{
			Description: "Error message",
			Type:        []string{"string"},
		},
	}
	defs["error"] = errSchema
}

func (c *ABI2Swagger) getDeclaredIDDetails(inst bool, declaredID string, inputs abi.Arguments, devdocs gjson.Result) (bool, string, string, gjson.Result) {
	sig := declaredID
	constructor := (declaredID == "constructor")
	path := "/"
	if !constructor {
		if inst {
			path = "/" + declaredID
		} else {
			path = "/{address}/" + declaredID
		}
	}
	sig += "("
	for i, input := range inputs {
		if i > 0 {
			sig += ","
		}
		sig += input.Type.String()
	}
	sig += ")"
	search := strings.ReplaceAll(sig, "(", "\\(")
	search = strings.ReplaceAll(sig, ")", "\\)")
	methodDocs := devdocs.Get(search)
	return constructor, sig, path, methodDocs
}

func (c *ABI2Swagger) buildMethodDefinitionsAndPath(inst bool, defs map[string]spec.Schema, paths map[string]spec.PathItem, name string, method abi.Method, devdocs gjson.Result) {

	constructor, methodSig, path, methodDocs := c.getDeclaredIDDetails(inst, name, method.Inputs, devdocs)
	if method.Const {
		methodSig += " [read only]"
	}

	inputSchema := url.QueryEscape(name) + "_inputs"
	outputSchema := url.QueryEscape(name) + "_outputs"
	c.buildArgumentsDefinition(defs, outputSchema, method.Outputs, methodDocs)
	pathItem := spec.PathItem{}
	if !constructor {
		pathItem.Get = c.buildGETPath(outputSchema, inst, name, method, methodSig, methodDocs)
	}
	c.buildArgumentsDefinition(defs, inputSchema, method.Inputs, methodDocs)
	pathItem.Post = c.buildPOSTPath(inputSchema, outputSchema, inst, constructor, name, method, methodSig, methodDocs)
	paths[path] = pathItem

	return
}

func (c *ABI2Swagger) addRegisterPath(paths map[string]spec.PathItem) {
	pathItem := spec.PathItem{}
	registerParam, _ := spec.NewRef("#/parameters/registerParam")
	pathItem.Post = &spec.Operation{
		OperationProps: spec.OperationProps{
			ID:          "registerAddress",
			Summary:     "Register an existing contract address",
			Description: "Add a friendly path for an instance of this contract already deployed to the chain",
			Consumes:    []string{"application/json", "application/x-yaml"},
			Produces:    []string{"application/json"},
			Responses: &spec.Responses{
				ResponsesProps: spec.ResponsesProps{
					StatusCodeResponses: map[int]spec.Response{
						201: spec.Response{
							ResponseProps: spec.ResponseProps{
								Description: "Successfully registered",
							},
						},
					},
				},
			},
			Parameters: []spec.Parameter{
				c.getAddressParam(),
				spec.Parameter{
					Refable: spec.Refable{
						Ref: registerParam,
					},
				},
				spec.Parameter{
					ParamProps: spec.ParamProps{
						Name:     "body",
						In:       "body",
						Required: true,
						Schema: &spec.Schema{
							SchemaProps: spec.SchemaProps{
								Description: "Registration request",
								Type:        []string{"object"},
								Properties:  map[string]spec.Schema{ /* currently no properties - more metadata and version control TBD */ },
							},
						},
					},
				},
			},
		},
	}
	paths["/{address}"] = pathItem
}

func (c *ABI2Swagger) buildEventDefinitionsAndPath(inst bool, defs map[string]spec.Schema, paths map[string]spec.PathItem, name string, event abi.Event, devdocs gjson.Result) {
	_, eventSig, path, eventDocs := c.getDeclaredIDDetails(inst, event.Name, event.Inputs, devdocs)
	eventSig += " [event]"
	pathItem := spec.PathItem{}
	eventSchema := url.QueryEscape(name) + "_event"
	c.buildArgumentsDefinition(defs, eventSchema, event.Inputs, eventDocs)
	pathItem.Post = c.buildEventPOSTPath(eventSchema, inst, event, eventSig, eventDocs)
	paths[path+"/subscribe"] = pathItem
	return
}

func (c *ABI2Swagger) getCommonParameters() map[string]spec.Parameter {
	params := make(map[string]spec.Parameter)
	params["fromParam"] = spec.Parameter{
		ParamProps: spec.ParamProps{
			Description: "The 'from' address (header: x-kaleido-from)",
			Name:        "kld-from",
			In:          "query",
			Required:    false,
		},
		SimpleSchema: spec.SimpleSchema{
			Type: "string",
		},
	}
	params["valueParam"] = spec.Parameter{
		ParamProps: spec.ParamProps{
			Description:     "Ether value to send with the transaction (header: x-kaleido-ethvalue)",
			Name:            "kld-ethvalue",
			In:              "query",
			Required:        false,
			AllowEmptyValue: true,
		},
		SimpleSchema: spec.SimpleSchema{
			Type: "integer",
		},
	}
	params["gasParam"] = spec.Parameter{
		ParamProps: spec.ParamProps{
			Description:     "Gas to send with the transaction (auto-calculated if not set) (header: x-kaleido-gas)",
			Name:            "kld-gas",
			In:              "query",
			Required:        false,
			AllowEmptyValue: true,
		},
		SimpleSchema: spec.SimpleSchema{
			Type: "integer",
		},
	}
	params["gaspriceParam"] = spec.Parameter{
		ParamProps: spec.ParamProps{
			Description:     "Gas Price offered (header: x-kaleido-gasprice)",
			Name:            "kld-gasprice",
			In:              "query",
			Required:        false,
			AllowEmptyValue: true,
		},
		SimpleSchema: spec.SimpleSchema{
			Type: "integer",
		},
	}
	params["syncParam"] = spec.Parameter{
		ParamProps: spec.ParamProps{
			Description:     "Block the HTTP request until the tx is mined (does not store the receipt) (header: x-kaleido-sync)",
			Name:            "kld-sync",
			In:              "query",
			Required:        false,
			AllowEmptyValue: true,
		},
		SimpleSchema: spec.SimpleSchema{
			Type:    "boolean",
			Default: true,
		},
	}
	params["callParam"] = spec.Parameter{
		ParamProps: spec.ParamProps{
			Description:     "Perform a read-only call with the same parameters that would be used to invoke, and return result (header: x-kaleido-call)",
			Name:            "kld-call",
			In:              "query",
			Required:        false,
			AllowEmptyValue: true,
		},
		SimpleSchema: spec.SimpleSchema{
			Type: "boolean",
		},
	}
	params["privateFromParam"] = spec.Parameter{
		ParamProps: spec.ParamProps{
			Description:     "Private transaction sender (header: x-kaleido-privatefrom)",
			Name:            "kld-privatefrom",
			In:              "query",
			Required:        false,
			AllowEmptyValue: false,
		},
		SimpleSchema: spec.SimpleSchema{
			Type: "string",
		},
	}
	params["privateForParam"] = spec.Parameter{
		ParamProps: spec.ParamProps{
			Description:     "Private transaction recipients (comma separated or multiple params) (header: x-kaleido-privatefor)",
			Name:            "kld-privatefor",
			In:              "query",
			Required:        false,
			AllowEmptyValue: false,
		},
		SimpleSchema: spec.SimpleSchema{
			Type: "string",
		},
	}
	params["privacyGroupIdParam"] = spec.Parameter{
		ParamProps: spec.ParamProps{
			Description:     "Private transaction group ID (header: x-kaleido-privacyGroupId)",
			Name:            "kld-privacygroupid",
			In:              "query",
			Required:        false,
			AllowEmptyValue: false,
		},
		SimpleSchema: spec.SimpleSchema{
			Type: "string",
		},
	}
	params["registerParam"] = spec.Parameter{
		ParamProps: spec.ParamProps{
			Description:     "Register the installed contract on a friendly path (overwrites existing) (header: x-kaleido-register)",
			Name:            "kld-register",
			In:              "query",
			Required:        false,
			AllowEmptyValue: false,
		},
		SimpleSchema: spec.SimpleSchema{
			Type: "string",
		},
	}
	params["blocknumberParam"] = spec.Parameter{
		ParamProps: spec.ParamProps{
			Description:     "The target block number for eth_call requests. One of 'earliest/latest/pending', a number or a hex string (header: x-kaleido-blocknumber)",
			Name:            "kld-blocknumber",
			In:              "query",
			Required:        false,
			AllowEmptyValue: false,
		},
		SimpleSchema: spec.SimpleSchema{
			Type: "string",
		},
	}
	return params
}

func (c *ABI2Swagger) addCommonParams(op *spec.Operation, isPOST bool, isConstructor bool) {

	op.Security = append(op.Security, map[string][]string{kaleidoAppCredential: []string{}})

	fromParam, _ := spec.NewRef("#/parameters/fromParam")
	valueParam, _ := spec.NewRef("#/parameters/valueParam")
	gasParam, _ := spec.NewRef("#/parameters/gasParam")
	gaspriceParam, _ := spec.NewRef("#/parameters/gaspriceParam")
	syncParam, _ := spec.NewRef("#/parameters/syncParam")
	callParam, _ := spec.NewRef("#/parameters/callParam")
	privateFromParam, _ := spec.NewRef("#/parameters/privateFromParam")
	privateForParam, _ := spec.NewRef("#/parameters/privateForParam")
	privacyGroupIDParam, _ := spec.NewRef("#/parameters/privacyGroupIdParam")
	registerParam, _ := spec.NewRef("#/parameters/registerParam")
	blocknumberParam, _ := spec.NewRef("#/parameters/blocknumberParam")
	op.Parameters = append(op.Parameters, spec.Parameter{
		Refable: spec.Refable{
			Ref: fromParam,
		},
	})
	op.Parameters = append(op.Parameters, spec.Parameter{
		Refable: spec.Refable{
			Ref: valueParam,
		},
	})
	op.Parameters = append(op.Parameters, spec.Parameter{
		Refable: spec.Refable{
			Ref: gasParam,
		},
	})
	op.Parameters = append(op.Parameters, spec.Parameter{
		Refable: spec.Refable{
			Ref: gaspriceParam,
		},
	})
	if isPOST {
		op.Parameters = append(op.Parameters, spec.Parameter{
			Refable: spec.Refable{
				Ref: syncParam,
			},
		})
		op.Parameters = append(op.Parameters, spec.Parameter{
			Refable: spec.Refable{
				Ref: callParam,
			},
		})
		op.Parameters = append(op.Parameters, spec.Parameter{
			Refable: spec.Refable{
				Ref: privateFromParam,
			},
		})
		op.Parameters = append(op.Parameters, spec.Parameter{
			Refable: spec.Refable{
				Ref: privateForParam,
			},
		})
		op.Parameters = append(op.Parameters, spec.Parameter{
			Refable: spec.Refable{
				Ref: blocknumberParam,
			},
		})
		if c.orionPrivateAPI {
			op.Parameters = append(op.Parameters, spec.Parameter{
				Refable: spec.Refable{
					Ref: privacyGroupIDParam,
				},
			})
		}
	}
	if isConstructor {
		op.Parameters = append(op.Parameters, spec.Parameter{
			Refable: spec.Refable{
				Ref: registerParam,
			},
		})
	}
}

func (c *ABI2Swagger) getAddressParam() spec.Parameter {
	return spec.Parameter{
		ParamProps: spec.ParamProps{
			Description: "The contract address",
			Name:        "address",
			In:          "path",
			Required:    true,
		},
		SimpleSchema: spec.SimpleSchema{
			Type: "string",
		},
	}
}

func (c *ABI2Swagger) buildGETPath(outputSchema string, inst bool, name string, method abi.Method, methodSig string, devdocs gjson.Result) *spec.Operation {
	parameters := make([]spec.Parameter, 0, len(method.Inputs)+1)
	if !inst {
		parameters = append(parameters, c.getAddressParam())
	}
	for _, input := range method.Inputs {
		desc := devdocs.Get("params." + input.Name).String()
		varDetails := desc
		if varDetails != "" {
			varDetails = ": " + desc
		}
		parameters = append(parameters, spec.Parameter{
			ParamProps: spec.ParamProps{
				Name:        input.Name,
				In:          "query",
				Description: input.Type.String() + varDetails,
				Required:    true,
			},
			SimpleSchema: spec.SimpleSchema{
				Type: "string",
			},
		})
	}
	op := &spec.Operation{
		OperationProps: spec.OperationProps{
			ID:          name + "_get",
			Summary:     methodSig,
			Description: devdocs.Get("details").String(),
			Produces:    []string{"application/json"},
			Responses:   c.buildResponses(outputSchema, devdocs),
			Parameters:  parameters,
		},
	}
	c.addCommonParams(op, false, false)
	return op
}

func (c *ABI2Swagger) buildPOSTPath(inputSchema, outputSchema string, inst, constructor bool, name string, method abi.Method, methodSig string, devdocs gjson.Result) *spec.Operation {
	parameters := make([]spec.Parameter, 0, 2)
	if !inst && !constructor {
		parameters = append(parameters, spec.Parameter{
			ParamProps: spec.ParamProps{
				Description: "The contract address",
				Name:        "address",
				In:          "path",
				Required:    true,
			},
			SimpleSchema: spec.SimpleSchema{
				Type: "string",
			},
		})
	}
	ref, _ := jsonreference.New("#/definitions/" + inputSchema)
	parameters = append(parameters, spec.Parameter{
		ParamProps: spec.ParamProps{
			Name:     "body",
			In:       "body",
			Required: true,
			Schema: &spec.Schema{
				SchemaProps: spec.SchemaProps{
					Ref: spec.Ref{
						Ref: ref,
					},
				},
			},
		},
	})
	op := &spec.Operation{
		OperationProps: spec.OperationProps{
			ID:          name + "_post",
			Summary:     methodSig,
			Description: devdocs.Get("details").String(),
			Consumes:    []string{"application/json", "application/x-yaml"},
			Produces:    []string{"application/json"},
			Responses:   c.buildResponses(outputSchema, devdocs),
			Parameters:  parameters,
		},
	}
	c.addCommonParams(op, true, constructor)
	return op
}

func (c *ABI2Swagger) buildEventPOSTPath(eventSchema string, inst bool, event abi.Event, eventSig string, devdocs gjson.Result) *spec.Operation {
	parameters := make([]spec.Parameter, 0, 2)
	id := event.Name + "_subscribe"
	if !inst {
		parameters = append(parameters, spec.Parameter{
			ParamProps: spec.ParamProps{
				Description: "The contract address",
				Name:        "address",
				In:          "path",
				Required:    true,
			},
			SimpleSchema: spec.SimpleSchema{
				Type: "string",
			},
		})
		id = event.Name + "_subscribe_all"
	}
	parameters = append(parameters, spec.Parameter{
		ParamProps: spec.ParamProps{
			Name:        "body",
			In:          "body",
			Description: "Subscription configuration for the REST Gateway (response schema will be delivered async over the configured stream)",
			Required:    true,
			Schema: &spec.Schema{
				SchemaProps: spec.SchemaProps{
					Properties: map[string]spec.Schema{
						"stream": spec.Schema{
							SchemaProps: spec.SchemaProps{
								Description: "The ID of an event stream already configured in the REST Gateway",
								Type:        []string{"string"},
							},
						},
						"fromBlock": spec.Schema{
							SchemaProps: spec.SchemaProps{
								Description: "The block number to start the subscription from",
								Type:        []string{"string"},
								Default:     "latest",
							},
						},
					},
				},
			},
		},
	})
	op := &spec.Operation{
		OperationProps: spec.OperationProps{
			ID:          id,
			Summary:     eventSig,
			Description: devdocs.Get("details").String(),
			Consumes:    []string{"application/json", "application/x-yaml"},
			Produces:    []string{"application/json"},
			Responses:   c.buildResponses(eventSchema, devdocs),
			Parameters:  parameters,
		},
	}
	return op
}

func (c *ABI2Swagger) buildResponses(outputSchema string, devdocs gjson.Result) *spec.Responses {
	errRef, _ := jsonreference.New("#/definitions/error")
	errorResponse := spec.Response{
		ResponseProps: spec.ResponseProps{
			Description: "error",
			Schema: &spec.Schema{
				SchemaProps: spec.SchemaProps{
					Ref: spec.Ref{
						Ref: errRef,
					},
				},
			},
		},
	}
	outputRef, _ := jsonreference.New("#/definitions/" + outputSchema)
	desc := devdocs.Get("return").String()
	if desc == "" {
		desc = "successful response"
	}
	return &spec.Responses{
		ResponsesProps: spec.ResponsesProps{
			StatusCodeResponses: map[int]spec.Response{
				200: spec.Response{
					ResponseProps: spec.ResponseProps{
						Description: desc,
						Schema: &spec.Schema{
							SchemaProps: spec.SchemaProps{
								Ref: spec.Ref{
									Ref: outputRef,
								},
							},
						},
					},
				},
			},
			Default: &errorResponse,
		},
	}
}

func (c *ABI2Swagger) buildArgumentsDefinition(defs map[string]spec.Schema, name string, args abi.Arguments, devdocs gjson.Result) {

	s := spec.Schema{
		SchemaProps: spec.SchemaProps{
			Type:       []string{"object"},
			Properties: make(map[string]spec.Schema),
		},
	}
	defs[name] = s

	for idx, arg := range args {
		argName := arg.Name
		if argName == "" {
			argName = "output"
			if idx != 0 {
				argName += strconv.Itoa(idx)
			}
		}
		argDocs := devdocs.Get("params." + arg.Name)
		s.Properties[argName] = c.mapArgToSchema(arg, argDocs.String())
	}

}

func (c *ABI2Swagger) mapArgToSchema(arg abi.Argument, desc string) spec.Schema {

	varDetails := desc
	if varDetails != "" {
		varDetails = ": " + desc
	}

	s := spec.Schema{
		SchemaProps: spec.SchemaProps{
			Description: arg.Type.String() + varDetails,
			Type:        []string{"string"},
		},
	}
	c.mapTypeToSchema(&s, arg.Type)

	return s
}

func (c *ABI2Swagger) mapTypeToSchema(s *spec.Schema, t abi.Type) {

	switch t.T {
	case abi.IntTy, abi.UintTy:
		s.Type = []string{"string"}
		s.Pattern = "^-?[0-9]+$"
		// We would like to indicate we support numbers in this field, but neither
		// type arrays or oneOf seem to work with the tooling
		break
	case abi.BoolTy:
		s.Type = []string{"boolean"}
		break
	case abi.AddressTy:
		s.Type = []string{"string"}
		s.Pattern = "^(0x)?[a-fA-F0-9]{40}$"
		break
	case abi.StringTy:
		s.Type = []string{"string"}
		break
	case abi.BytesTy:
		s.Type = []string{"string"}
		s.Pattern = "^(0x)?[a-fA-F0-9]+$"
		break
	case abi.FixedBytesTy:
		s.Type = []string{"string"}
		s.Pattern = "^(0x)?[a-fA-F0-9]{" + strconv.Itoa(t.Size*2) + "}$"
		break
	case abi.SliceTy, abi.ArrayTy:
		s.Type = []string{"array"}
		s.Items = &spec.SchemaOrArray{}
		s.Items.Schema = &spec.Schema{}
		c.mapTypeToSchema(s.Items.Schema, *t.Elem)
		break
	}

}
