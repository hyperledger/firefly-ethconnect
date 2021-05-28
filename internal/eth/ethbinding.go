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

package eth

import (
	"fmt"
	"os"
	"path"
	"plugin"
	"strings"

	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	log "github.com/sirupsen/logrus"
)

const (
	ethbindingFileEnvVar = "ETHCONNECT_ETHBINDING_FILE"
)

var API ethbinding.EthAPI

// initializes the dynamicallly linked imports from go-ethereum
func init() {
	log.Debugf("Loading ethbinding.so")

	bindingLocation := os.Getenv(ethbindingFileEnvVar)
	if bindingLocation == "" {
		cwd, _ := os.Getwd()
		// If we are in a subdirectory of internal, then assume we're running unit tests
		if strings.HasSuffix(cwd, "cmd") {
			// Use the project root
			bindingLocation = fmt.Sprintf("%s/../ethbinding.so", cwd)
		} else if strings.HasSuffix(path.Dir(cwd), "internal") {
			// Use the project root
			bindingLocation = fmt.Sprintf("%s/../../ethbinding.so", cwd)
		} else {
			// Use the local directory
			bindingLocation = fmt.Sprintf("%s/ethbinding.so", cwd)
		}
	}

	ethapiPlugin, err := plugin.Open(bindingLocation)
	if err != nil {
		panic(fmt.Errorf("failed to load '%s': %s", bindingLocation, err))
	}
	log.Debugf("Loaded '%s'", bindingLocation)

	ethapiSymbol, err := ethapiPlugin.Lookup("EthAPIShim")
	if err != nil || ethapiSymbol == nil {
		panic(fmt.Errorf("failed to EthAPI from '%s': %s", bindingLocation, err))
	}
	log.Debugf("Loaded EthAPI: %+v", ethapiSymbol)

	API = *ethapiSymbol.(*ethbinding.EthAPI)
}
