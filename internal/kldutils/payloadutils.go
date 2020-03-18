// Copyright 2018, 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldutils

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/icza/dyno"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
)

const (
	// MaxPayloadSize max size of content
	MaxPayloadSize = 1024 * 1024
)

// YAMLorJSONPayload processes either a YAML or JSON payload from an input HTTP request
func YAMLorJSONPayload(req *http.Request) (map[string]interface{}, error) {

	if req.ContentLength > MaxPayloadSize {
		return nil, klderrors.Errorf(klderrors.HelperYAMLorJSONPayloadTooLarge)
	}
	originalPayload, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return nil, klderrors.Errorf(klderrors.HelperYAMLorJSONPayloadReadFailed, err)
	}

	// We support both YAML and JSON input.
	// We parse the message into a generic string->interface map, that lets
	// us check a couple of routing fields needed to dispatch the messages
	// to Kafka (always in JSON). However, we do not perform full parsing.
	var msg map[string]interface{}
	contentType := strings.ToLower(req.Header.Get("Content-type"))
	log.Infof("Received message 'Content-Type: %s' Length: %d", contentType, req.ContentLength)
	if req.ContentLength == 0 {
		return map[string]interface{}{}, nil
	}

	// Unless explicitly declared as YAML, try JSON first
	var unmarshalledAsJSON = false
	if contentType != "application/x-yaml" && contentType != "text/yaml" {
		err := json.Unmarshal(originalPayload, &msg)
		if err != nil {
			log.Debugf("Payload is not valid JSON - trying YAML: %s", err)
		} else {
			unmarshalledAsJSON = true
		}
	}
	// Try YAML if content-type is set, or if JSON fails
	if !unmarshalledAsJSON {
		yamlGenericPayload := make(map[interface{}]interface{})
		err := yaml.Unmarshal(originalPayload, &yamlGenericPayload)
		if err != nil {
			return nil, klderrors.Errorf(klderrors.HelperYAMLorJSONPayloadParseFailed, err)
		}
		msg = dyno.ConvertMapI2MapS(yamlGenericPayload).(map[string]interface{})
	}
	return msg, nil
}
