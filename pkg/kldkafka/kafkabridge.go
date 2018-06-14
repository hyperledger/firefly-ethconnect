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

package kldkafka

import (
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// KafkaBridge is the Kaleido go-ethereum exerciser
type KafkaBridge struct {
}

// CobraInit retruns a cobra command to configure this Kafka
func (k *KafkaBridge) CobraInit() (cmd *cobra.Command) {
	cmd = &cobra.Command{
		Use:   "kafka",
		Short: "Kafka bridge to Ethereum",
		Run: func(cmd *cobra.Command, args []string) {
			if err := k.Start(); err != nil {
				log.Error("Kafka Bridge Start: ", err)
				os.Exit(1)
			}
		},
	}
	return
}

// Start kicks off the bridge
func (k *KafkaBridge) Start() (err error) {
	log.Infof("KafkaBridge started")
	return
}
