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
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/Shopify/sarama"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// KafkaBridge is the Kaleido go-ethereum exerciser
type KafkaBridge struct {
	Conf struct {
		Brokers            []string
		InsecureSkipVerify bool
		TLS                struct {
			ClientCertsFile string
			CACertsFile     string
			Enabled         bool
			PrivateKeyFile  string
		}
		Topic string
	}
	Client sarama.Client
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
	defBrokerList := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	defTLS, _ := strconv.ParseBool(os.Getenv("KAFKA_TLS"))
	cmd.Flags().StringArrayVarP(&k.Conf.Brokers, "brokers", "b", defBrokerList, "Comma-separated list of brokers")
	cmd.Flags().StringVarP(&k.Conf.TLS.ClientCertsFile, "clientcerts", "c", os.Getenv("KAFKA_TLS_CLIENT_CERT"), "A client certificate file, for mutual TLS auth")
	cmd.Flags().StringVarP(&k.Conf.TLS.PrivateKeyFile, "clientkey", "k", os.Getenv("KAFKA_TLS_CLIENT_KEY"), "A client private key file, for mutual TLS auth")
	cmd.Flags().StringVarP(&k.Conf.TLS.CACertsFile, "cacerts", "C", os.Getenv("KAFKA_TLS_CA_CERTS"), "CA certificates file (or host CAs will be used)")
	cmd.Flags().BoolVarP(&k.Conf.TLS.Enabled, "tls", "t", defTLS, "Enable TLS (SSL) on the connection")
	cmd.Flags().BoolVarP(&k.Conf.InsecureSkipVerify, "insecure", "z", defTLS, "Disable verification of TLS certificate chain")
	cmd.Flags().StringVarP(&k.Conf.Topic, "topic-in", "i", os.Getenv("KAFKA_TOPIC_IN"), "Topic to listen to")
	return
}

func (k *KafkaBridge) createTLSConfiguration() (t *tls.Config, err error) {

	mutualAuth := k.Conf.TLS.ClientCertsFile != "" && k.Conf.TLS.PrivateKeyFile != ""
	log.Debugf("TLS Enabled=%t Insecure=%t MutualAuth=%t ClientCertsFile=%s PrivateKeyFile=%s CACertsFile=%s",
		k.Conf.TLS.Enabled, k.Conf.InsecureSkipVerify, mutualAuth, k.Conf.TLS.ClientCertsFile, k.Conf.TLS.PrivateKeyFile, k.Conf.TLS.CACertsFile)
	if !k.Conf.TLS.Enabled {
		return
	}

	var clientCerts []tls.Certificate
	if mutualAuth {
		var cert tls.Certificate
		if cert, err = tls.LoadX509KeyPair(k.Conf.TLS.ClientCertsFile, k.Conf.TLS.PrivateKeyFile); err != nil {
			log.Errorf("Unable to load client key/certificate: %s", err)
			return
		}
		clientCerts = append(clientCerts, cert)
	}

	var caCertPool *x509.CertPool
	if k.Conf.TLS.CACertsFile != "" {
		var caCert []byte
		if caCert, err = ioutil.ReadFile(k.Conf.TLS.CACertsFile); err != nil {
			log.Errorf("Unable to load CA certificates: %s", err)
			return
		}
		caCertPool = x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
	}

	t = &tls.Config{
		Certificates:       clientCerts,
		RootCAs:            caCertPool,
		InsecureSkipVerify: k.Conf.InsecureSkipVerify,
	}
	return
}

func (k *KafkaBridge) connect() (err error) {
	var tlsConfig *tls.Config
	if tlsConfig, err = k.createTLSConfiguration(); err != nil {
		return
	}

	clientConf := sarama.NewConfig()
	clientConf.Net.TLS.Enable = (tlsConfig != nil)
	clientConf.Net.TLS.Config = tlsConfig

	log.Debugf("Kafka brokers: %s", k.Conf.Brokers)
	if k.Client, err = sarama.NewClient(k.Conf.Brokers, clientConf); err != nil {
		log.Errorf("Failed to create Kafka client: %s", err)
		return
	}

	return
}

// Start kicks off the bridge
func (k *KafkaBridge) Start() (err error) {

	if err = k.connect(); err != nil {
		return
	}

	log.Infof("KafkaBridge started")

	return
}
