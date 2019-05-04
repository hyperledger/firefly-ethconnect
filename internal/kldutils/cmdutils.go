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
	"os"
	"strconv"

	"gopkg.in/yaml.v2"

	log "github.com/sirupsen/logrus"
)

// AllOrNoneReqd util for checking parameters that must be provided together
func AllOrNoneReqd(opts ...string) (ok bool) {
	var setFlags, unsetFlags []string
	for _, opt := range opts {
		if opt != "" {
			setFlags = append(setFlags, opt)
		} else {
			unsetFlags = append(unsetFlags, opt)
		}
	}
	ok = !(len(setFlags) != 0 && len(unsetFlags) != 0)
	return
}

// DefInt defaults an integer to a value in an Env var, and if not the default integer provided
func DefInt(envVarName string, defValue int) int {
	defStr := os.Getenv(envVarName)
	if defStr == "" {
		return defValue
	}
	parsedInt, err := strconv.ParseInt(defStr, 10, 32)
	if err != nil {
		log.Errorf("Invalid string in env var %s", envVarName)
		return defValue
	}
	return int(parsedInt)
}

// MarshalToYAML marshals a JSON annotated structure into YAML, by first going to JSON
func MarshalToYAML(conf interface{}) (yamlBytes []byte, err error) {
	var jsonBytes []byte
	if jsonBytes, err = json.Marshal(conf); err != nil {
		return
	}
	jsonAsMap := make(map[string]interface{})
	if err = json.Unmarshal(jsonBytes, &jsonAsMap); err != nil {
		return
	}
	yamlBytes, err = yaml.Marshal(&jsonAsMap)
	return
}
