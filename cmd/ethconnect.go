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

package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/icza/dyno"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldkafka"
	"github.com/kaleido-io/ethconnect/internal/kldrest"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

// ServerConfig is the parent YAML structure that configures ethconnect
// to run with a set of individual commands as goroutines
// (rather than the simple commandline mode that runs a single command)
type ServerConfig struct {
	KafkaBridges map[string]*kldkafka.KafkaBridgeConf `json:"kafka"`
	Webhooks     map[string]*kldrest.RESTGatewayConf  `json:"webhooks"`
	RESTGateways map[string]*kldrest.RESTGatewayConf  `json:"rest"`
	Plugins      PluginConfig                         `json:"plugins"`
}

func initLogging(debugLevel int) {
	log.SetFormatter(&prefixed.TextFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		DisableSorting:  true,
		ForceFormatting: true,
		FullTimestamp:   true,
	})
	switch debugLevel {
	case 0:
		log.SetLevel(log.ErrorLevel)
		break
	case 1:
		log.SetLevel(log.InfoLevel)
		break
	case 2:
		log.SetLevel(log.DebugLevel)
		break
	case 3:
		log.SetLevel(log.TraceLevel)
		break
	default:
		log.SetLevel(log.DebugLevel)
		break
	}
	log.Debugf("Log level set to %d", debugLevel)
}

var rootConfig struct {
	DebugLevel int
	PrintYAML  bool
}

var serverCmdConfig struct {
	Filename string
	Type     string
}

var rootCmd = &cobra.Command{
	Use:   "ethconnect [sub]",
	Short: "Connectivity Bridge for Ethereum permissioned chains",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		initLogging(rootConfig.DebugLevel)
	},
}

func initServer() (serverCmd *cobra.Command) {
	serverCmd = &cobra.Command{
		Use:   "server",
		Short: "Runs all of the bridges defined in a YAML config file",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			err = startServer()
			return
		},
		PreRunE: func(cmd *cobra.Command, args []string) (err error) {
			if serverCmdConfig.Filename == "" {
				err = klderrors.Errorf(klderrors.ConfigNoYAML)
				return
			}
			return
		},
	}
	defType := os.Getenv("ETHCONNECT_CONFIGFILE_TYPE")
	if defType == "" {
		defType = "yaml"
	}
	serverCmd.Flags().StringVarP(&serverCmdConfig.Filename, "filename", "f", os.Getenv("ETHCONNECT_CONFIGFILE"), "Configuration file")
	serverCmd.Flags().StringVarP(&serverCmdConfig.Type, "type", "t", defType, "File type (json/yaml)")
	return
}

func readServerConfig() (serverConfig *ServerConfig, err error) {
	confBytes, err := ioutil.ReadFile(serverCmdConfig.Filename)
	if err != nil {
		err = klderrors.Errorf(klderrors.ConfigFileReadFailed, serverCmdConfig.Filename, err)
		return
	}
	if strings.ToLower(serverCmdConfig.Type) == "yaml" {
		// Convert to JSON first
		yamlGenericPayload := make(map[interface{}]interface{})
		if err = yaml.Unmarshal(confBytes, &yamlGenericPayload); err != nil {
			err = klderrors.Errorf(klderrors.ConfigYAMLParseFile, serverCmdConfig.Filename, err)
			return
		}
		genericPayload := dyno.ConvertMapI2MapS(yamlGenericPayload).(map[string]interface{})
		// Reseialize back to JSON
		confBytes, _ = json.Marshal(&genericPayload)
	}
	serverConfig = &ServerConfig{}
	err = json.Unmarshal(confBytes, serverConfig)
	if err != nil {
		err = klderrors.Errorf(klderrors.ConfigYAMLPostParseFile, serverCmdConfig.Filename, err)
		return
	}

	// Load any plugins
	err = loadPlugins(&serverConfig.Plugins)

	return
}

func startServer() (err error) {
	serverConfig, err := readServerConfig()
	if err != nil {
		return
	}

	if rootConfig.PrintYAML {
		b, err := kldutils.MarshalToYAML(&serverConfig)
		print("# Full YAML configuration processed from supplied file\n" + string(b))
		return err
	}

	anyRoutineFinished := make(chan bool)

	var dontPrintYaml = false
	for name, conf := range serverConfig.KafkaBridges {
		kafkaBridge := kldkafka.NewKafkaBridge(&dontPrintYaml)
		kafkaBridge.SetConf(conf)
		if err := kafkaBridge.ValidateConf(); err != nil {
			return err
		}
		go func(name string, anyRoutineFinished chan bool) {
			log.Infof("Starting Kafka->Ethereum bridge '%s'", name)
			if err := kafkaBridge.Start(); err != nil {
				log.Errorf("Kafka->Ethereum bridge failed: %s", err)
			}
			anyRoutineFinished <- true
		}(name, anyRoutineFinished)
	}
	// Merge in legacy named 'webbhooks' configs
	if serverConfig.RESTGateways == nil {
		serverConfig.RESTGateways = make(map[string]*kldrest.RESTGatewayConf)
	}
	for name, conf := range serverConfig.Webhooks {
		serverConfig.RESTGateways[name] = conf
	}
	for name, conf := range serverConfig.RESTGateways {
		restGateway := kldrest.NewRESTGateway(&dontPrintYaml)
		restGateway.SetConf(conf)
		if err := restGateway.ValidateConf(); err != nil {
			return err
		}
		go func(name string, anyRoutineFinished chan bool) {
			log.Infof("Starting REST gateway '%s'", name)
			if err := restGateway.Start(); err != nil {
				log.Errorf("REST gateway failed: %s", err)
			}
			anyRoutineFinished <- true
		}(name, anyRoutineFinished)
	}

	// Terminate when ANY routine fails (do not wait for them all to complete)
	<-anyRoutineFinished

	return
}

func init() {
	rootCmd.PersistentFlags().IntVarP(&rootConfig.DebugLevel, "debug", "d", 1, "0=error, 1=info, 2=debug")
	rootCmd.PersistentFlags().BoolVarP(&rootConfig.PrintYAML, "print-yaml-confg", "Y", false, "Print YAML config snippet and exit")

	serverCmd := initServer()
	rootCmd.AddCommand(serverCmd)

	kafkaBridge := kldkafka.NewKafkaBridge(&rootConfig.PrintYAML)
	rootCmd.AddCommand(kafkaBridge.CobraInit())

	restGateway := kldrest.NewRESTGateway(&rootConfig.PrintYAML)
	rootCmd.AddCommand(restGateway.CobraInit("webhooks")) // for backwards compatibility
	rootCmd.AddCommand(restGateway.CobraInit("rest"))
}

// Execute is called by the main method of the package
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		return 1
	}
	return 0
}
