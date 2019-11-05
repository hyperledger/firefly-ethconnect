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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	log "github.com/sirupsen/logrus"
)

// HTTPRequester perofrms common HTTP request logging/processing for utilities
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
			return nil, fmt.Errorf("Failed to serialize request payload: %s", ehr)
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
		return nil, fmt.Errorf("Error querying %s", hr.name)
	}
	log.Infof("%s %s <-- [%d]", method, url, res.StatusCode)
	if res.StatusCode == 404 {
		return nil, nil
	}
	var jsonBody map[string]interface{}
	if res.StatusCode == 204 {
		jsonBody = make(map[string]interface{})
	} else {
		resBody, ehr := ioutil.ReadAll(res.Body)
		if ehr = json.Unmarshal(resBody, &jsonBody); ehr != nil {
			log.Errorf("%s %s <-- [%d] !Failed to read body: %s", method, url, res.StatusCode, ehr)
			return nil, fmt.Errorf("Could not process %s [%d] response", hr.name, res.StatusCode)
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			log.Errorf("%s %s <-- [%d]: %+v", method, url, res.StatusCode, jsonBody)
			if ehrMsg, ok := jsonBody["errorMessage"]; ok {
				return nil, fmt.Errorf("%s returned [%d]: %s", hr.name, res.StatusCode, ehrMsg)
			}
			return nil, fmt.Errorf("Error querying %s", hr.name)
		}
	}
	return jsonBody, nil
}

// GetResponseString returns a string from a response map, asserting its existencer
func (hr *HTTPRequester) GetResponseString(m map[string]interface{}, p string, emptyOK bool) (string, error) {
	genericVal, exists := m[p]
	if !exists {
		return "", fmt.Errorf("'%s' missing in %s response", p, hr.name)
	}
	var stringVal string
	switch genericVal.(type) {
	case string:
		stringVal = genericVal.(string)
	case nil:
		stringVal = ""
	default:
		return "", fmt.Errorf("'%s' not a string in %s response", p, hr.name)
	}
	if !emptyOK && stringVal == "" {
		return "", fmt.Errorf("'%s' empty (or null) in %s response", p, hr.name)
	}
	return stringVal, nil
}
