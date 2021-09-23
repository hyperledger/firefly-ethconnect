// Copyright 2018, 2021 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	log "github.com/sirupsen/logrus"
)

// HTTPRequester performs common HTTP request logging/processing for utilities
type HTTPRequester struct {
	name   string
	client *http.Client
	conf   *HTTPRequesterConf
}

// HTTPRequesterConf configuration for making HTTP reuqests
type HTTPRequesterConf struct {
	Headers map[string][]string `json:"headers"`
}

// NewHTTPRequester constructor
func NewHTTPRequester(name string, conf *HTTPRequesterConf) *HTTPRequester {
	return &HTTPRequester{
		name: name,
		conf: conf,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns: 1,
			},
		},
	}
}

// DoRequest performs a single HTTP request processing the response as JSON
func (hr *HTTPRequester) DoRequest(method, url string, bodyMap map[string]interface{}) (map[string]interface{}, error) {
	log.Infof("%s %s -->", method, url)
	var body io.Reader
	if bodyMap != nil {
		bodyBytes, ehr := json.Marshal(bodyMap)
		if ehr != nil {
			return nil, errors.Errorf(errors.HTTPRequesterSerializeFailed, ehr)
		}
		body = bytes.NewReader(bodyBytes)
	}
	req, _ := http.NewRequest(method, url, body)
	req.Header = http.Header{}
	if hr.conf.Headers != nil {
		req.Header = hr.conf.Headers
	}
	req.Header.Add("content-type", "application/json")
	res, ehr := hr.client.Do(req)
	if ehr != nil {
		log.Errorf("%s %s <-- !Failed: %s", method, url, ehr)
		return nil, errors.Errorf(errors.HTTPRequesterNonStatusError, hr.name)
	}
	log.Infof("%s %s <-- [%d]", method, url, res.StatusCode)
	if res.StatusCode == 404 {
		return nil, nil
	}
	var jsonBody map[string]interface{}
	if res.StatusCode == 204 {
		jsonBody = make(map[string]interface{})
	} else {
		resBody, _ := ioutil.ReadAll(res.Body)
		if err := json.Unmarshal(resBody, &jsonBody); err != nil {
			log.Errorf("%s %s <-- [%d] !Failed to read body: %s", method, url, res.StatusCode, ehr)
			return nil, errors.Errorf(errors.HTTPRequesterStatusErrorNoData, hr.name, res.StatusCode)
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			log.Errorf("%s %s <-- [%d]: %+v", method, url, res.StatusCode, jsonBody)
			if ehrMsg, ok := jsonBody["errorMessage"]; ok {
				return nil, errors.Errorf(errors.HTTPRequesterStatusErrorWithData, hr.name, res.StatusCode, ehrMsg)
			}
			return nil, errors.Errorf(errors.HTTPRequesterStatusError, hr.name)
		}
	}
	return jsonBody, nil
}

// GetResponseString returns a string from a response map, asserting its existencer
func (hr *HTTPRequester) GetResponseString(m map[string]interface{}, p string, emptyOK bool) (string, error) {
	genericVal, exists := m[p]
	if !exists {
		return "", errors.Errorf(errors.HTTPRequesterResponseMissingField, p, hr.name)
	}
	var stringVal string
	switch genericVal.(type) {
	case string:
		stringVal = genericVal.(string)
	case nil:
		stringVal = ""
	default:
		return "", errors.Errorf(errors.HTTPRequesterResponseNonStringField, p, hr.name)
	}
	if !emptyOK && stringVal == "" {
		return "", errors.Errorf(errors.HTTPRequesterResponseNullField, p, hr.name)
	}
	return stringVal, nil
}
