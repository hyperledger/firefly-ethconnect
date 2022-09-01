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

package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/kafka"
	"github.com/hyperledger/firefly-ethconnect/internal/receipts"
	"github.com/hyperledger/firefly-ethconnect/internal/rest"
	"github.com/hyperledger/firefly-ethconnect/internal/utils"
	"github.com/icza/dyno"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"

	_ "net/http/pprof"
)

// ServerConfig is the parent YAML structure that configures ethconnect
// to run with a set of individual commands as goroutines
// (rather than the simple commandline mode that runs a single command)
type ServerConfig struct {
	KafkaBridges map[string]*kafka.KafkaBridgeConf `json:"kafka"`
	Webhooks     map[string]*rest.RESTGatewayConf  `json:"webhooks"`
	RESTGateways map[string]*rest.RESTGatewayConf  `json:"rest"`
	Plugins      PluginConfig                      `json:"plugins"`
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
	case 1:
		log.SetLevel(log.InfoLevel)
	case 2:
		log.SetLevel(log.DebugLevel)
	case 3:
		log.SetLevel(log.TraceLevel)
	default:
		log.SetLevel(log.DebugLevel)
	}
	log.Debugf("Log level set to %d", debugLevel)
}

var rootConfig struct {
	DebugLevel int
	DebugPort  int
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

		if rootConfig.DebugPort > 0 {
			go func() {
				log.Debugf("Debug HTTP endpoint listening on localhost:%d: %s", rootConfig.DebugPort, http.ListenAndServe(fmt.Sprintf("localhost:%d", rootConfig.DebugPort), nil))
			}()
		}
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
				err = errors.Errorf(errors.ConfigNoYAML)
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
		err = errors.Errorf(errors.ConfigFileReadFailed, serverCmdConfig.Filename, err)
		return
	}
	if strings.ToLower(serverCmdConfig.Type) == "yaml" {
		// Convert to JSON first
		yamlGenericPayload := make(map[interface{}]interface{})
		if err = yaml.Unmarshal(confBytes, &yamlGenericPayload); err != nil {
			err = errors.Errorf(errors.ConfigYAMLParseFile, serverCmdConfig.Filename, err)
			return
		}
		genericPayload := dyno.ConvertMapI2MapS(yamlGenericPayload).(map[string]interface{})
		// Reseialize back to JSON
		confBytes, _ = json.Marshal(&genericPayload)
	}
	serverConfig = &ServerConfig{}
	err = json.Unmarshal(confBytes, serverConfig)
	if err != nil {
		err = errors.Errorf(errors.ConfigYAMLPostParseFile, serverCmdConfig.Filename, err)
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
		b, err := utils.MarshalToYAML(&serverConfig)
		print("# Full YAML configuration processed from supplied file\n" + string(b))
		return err
	}

	anyRoutineFinished := make(chan bool)
	var dontPrintYaml = false

	// Merge in legacy named 'webbhooks' configs
	if serverConfig.RESTGateways == nil {
		serverConfig.RESTGateways = make(map[string]*rest.RESTGatewayConf)
	}
	for name, conf := range serverConfig.Webhooks {
		serverConfig.RESTGateways[name] = conf
	}
	var idempotencyCheckReceiptStore receipts.ReceiptStorePersistence
	restGateways := make(map[string]*rest.RESTGateway)
	for name, conf := range serverConfig.RESTGateways {
		restGateway := rest.NewRESTGateway(&dontPrintYaml)
		restGateways[name] = restGateway
		restGateway.SetConf(conf)
		if err := restGateway.ValidateConf(); err != nil {
			return err
		}
		// This is a slightly awkward cross-component call, to account for the most popular pattern of usage:
		// - Run in server mode
		// - Single REST API Gateway
		// - Single Kafka bridge co-located in the same process
		// In this scenario, we can pass the receipt store to the Kafka bridge for it to do
		// additional idempotency checks that prevent res-submission of transactions.
		if idempotencyCheckReceiptStore == nil {
			idempotencyCheckReceiptStore, err = restGateway.Init()
			if err != nil {
				return err
			}
		}

	}

	// Start the kafka bridges, passing in the receipt store if we have one
	for name, conf := range serverConfig.KafkaBridges {
		kafkaBridge := kafka.NewKafkaBridge(&dontPrintYaml)
		kafkaBridge.SetConf(conf)
		if err := kafkaBridge.ValidateConf(); err != nil {
			return err
		}
		go func(name string, anyRoutineFinished chan bool) {
			log.Infof("Starting Kafka->Ethereum bridge '%s'", name)
			if err := kafkaBridge.Start(idempotencyCheckReceiptStore); err != nil {
				log.Errorf("Kafka->Ethereum bridge failed: %s", err)
			}
			anyRoutineFinished <- true
		}(name, anyRoutineFinished)
	}

	// Start the rest gateways
	for name, rgw := range restGateways {
		restGateway := rgw
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
	rootCmd.PersistentFlags().IntVarP(&rootConfig.DebugPort, "debugPort", "Z", 6060, "Port for pprof HTTP endpoints (localhost only)")
	rootCmd.PersistentFlags().BoolVarP(&rootConfig.PrintYAML, "print-yaml-confg", "Y", false, "Print YAML config snippet and exit")

	serverCmd := initServer()
	rootCmd.AddCommand(serverCmd)

	kafkaBridge := kafka.NewKafkaBridge(&rootConfig.PrintYAML)
	rootCmd.AddCommand(kafkaBridge.CobraInit())

	restGateway := rest.NewRESTGateway(&rootConfig.PrintYAML)
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
