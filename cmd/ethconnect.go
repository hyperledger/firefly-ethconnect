// Copyright 2018 Kaleido, a ConsenSys business

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
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	"gopkg.in/yaml.v2"

	"github.com/kaleido-io/ethconnect/internal/kldkafka"
	"github.com/kaleido-io/ethconnect/internal/kldwebhooks"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// ServerConfig is the parent YAML structure that configures ethconnect
// to run with a set of individual commands as goroutines
// (rather than the simple commandline mode that runs a single command)
type ServerConfig struct {
	KafkaBridges    map[string]*kldkafka.KafkaBridgeConf       `yaml:"kafka"`
	WebhooksBridges map[string]*kldwebhooks.WebhooksBridgeConf `yaml:"webhooks"`
}

func initLogging(debugLevel int) {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	switch debugLevel {
	case 0:
		log.SetLevel(log.ErrorLevel)
		break
	case 1:
		log.SetLevel(log.InfoLevel)
		break
	default:
		log.SetLevel(log.DebugLevel)
		break
	}
	log.Debugf("Log level set to %d", debugLevel)
}

var rootConfig struct {
	DebugLevel int
}

var serverCmdConfig struct {
	Filename string
}

var rootCmd = &cobra.Command{
	Use: "ethconnect [sub]",
	Short: "Connectivity Bridge for Ethereum permissioned chains\n" +
		"Copyright (C) 2018 Kaleido, a ConsenSys business\n" +
		"Licensed under the Apache License, Version 2.0",
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
				err = fmt.Errorf("No YAML configuration filename specified")
				return
			}
			return
		},
	}
	serverCmd.Flags().StringVarP(&serverCmdConfig.Filename, "filename", "f", os.Getenv("ETHCONNECT_CONFIGFILE"), "YAML configuration file")
	return
}

func readServerConfig() (serverConfig *ServerConfig, err error) {
	yamlBytes, err := ioutil.ReadFile(serverCmdConfig.Filename)
	if err != nil {
		err = fmt.Errorf("Failed to read %s: %s", serverCmdConfig.Filename, err)
		return
	}
	serverConfig = &ServerConfig{}
	err = yaml.Unmarshal(yamlBytes, serverConfig)
	if err != nil {
		err = fmt.Errorf("Failed to process YAML config from %s: %s", serverCmdConfig.Filename, err)
		return
	}
	return
}

func startServer() (err error) {
	serverConfig, err := readServerConfig()
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	for name, conf := range serverConfig.KafkaBridges {
		kafkaBridge := kldkafka.NewKafkaBridge()
		kafkaBridge.SetConf(conf)
		if err := kafkaBridge.ValidateConf(); err != nil {
			return err
		}
		wg.Add(1)
		go func(name string) {
			log.Infof("Starting Kafka->Ethereum bridge '%s'", name)
			if err := kafkaBridge.Start(); err != nil {
				log.Errorf("Kafka->Ethereum bridge failed: %s", err)
			}
			wg.Done()
		}(name)
	}
	for name, conf := range serverConfig.WebhooksBridges {
		webhooksBridge := kldwebhooks.NewWebhooksBridge()
		webhooksBridge.SetConf(conf)
		if err := webhooksBridge.ValidateConf(); err != nil {
			return err
		}
		wg.Add(1)
		go func(name string) {
			log.Infof("Starting Webhooks->Kafka bridge '%s'", name)
			if err := webhooksBridge.Start(); err != nil {
				log.Errorf("Webhooks->Kafka bridge failed: %s", err)
			}
			wg.Done()
		}(name)
	}
	wg.Wait()

	return
}

func init() {
	rootCmd.PersistentFlags().IntVarP(&rootConfig.DebugLevel, "debug", "d", 1, "0=error, 1=info, 2=debug")

	serverCmd := initServer()
	rootCmd.AddCommand(serverCmd)

	kafkaBridge := kldkafka.NewKafkaBridge()
	rootCmd.AddCommand(kafkaBridge.CobraInit())

	webhooksBridge := kldwebhooks.NewWebhooksBridge()
	rootCmd.AddCommand(webhooksBridge.CobraInit())
}

// Execute is called by the main method of the package
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		return 1
	}
	return 0
}
