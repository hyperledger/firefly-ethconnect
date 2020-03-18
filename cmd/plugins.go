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

package cmd

import (
	"plugin"

	"github.com/kaleido-io/ethconnect/internal/kldauth"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/pkg/kldplugins"
	log "github.com/sirupsen/logrus"
)

// PluginConfig is the JSON configuration for loading plugins
type PluginConfig struct {
	SecurityModulePlugin string `json:"securityModule"`
}

func loadPlugins(conf *PluginConfig) error {
	if err := loadSecurityModulePlugin(conf); err != nil {
		return err
	}
	return nil
}

func loadSecurityModulePlugin(conf *PluginConfig) error {

	modulePath := conf.SecurityModulePlugin
	if modulePath == "" {
		return nil
	}

	log.Debugf("Loading SecurityModule plugin '%s'", modulePath)
	smPlugin, err := plugin.Open(modulePath)
	if err != nil {
		return klderrors.Errorf(klderrors.SecurityModulePluginLoad, err)
	}

	smSymbol, err := smPlugin.Lookup("SecurityModule")
	if err != nil || smSymbol == nil {
		return klderrors.Errorf(klderrors.SecurityModulePluginSymbol, modulePath, err)
	}

	kldauth.RegisterSecurityModule(*smSymbol.(*kldplugins.SecurityModule))
	return nil
}
