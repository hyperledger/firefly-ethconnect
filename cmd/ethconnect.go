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
	"os"

	"github.com/kaleido-io/ethconnect/pkg/kldkafka"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

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

var kafkaBridge kldkafka.KafkaBridge

var rootConfig struct {
	DebugLevel int
}

var rootCmd = &cobra.Command{
	Use: "ethconnect [sub]",
	Short: "Connectivity Bridge for Ethereum permissioned chains\n" +
		"Copyright (C) 2018 Kaleido, a ConsenSys business\n" +
		"Licensed under the Apache License, Version 2.0",
	TraverseChildren: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		initLogging(rootConfig.DebugLevel)
	},
}

func init() {

	rootCmd.Flags().IntVarP(&rootConfig.DebugLevel, "debug", "d", 1, "0=error, 1=info, 2=debug")
	rootCmd.AddCommand(kafkaBridge.CobraInit())
}

// Execute is called by the main method of the package
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
