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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/Shopify/sarama"
	cluster "github.com/bsm/sarama-cluster"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/kaleido-io/ethconnect/pkg/kldeth"
	"github.com/kaleido-io/ethconnect/pkg/kldmessages"
	"github.com/kaleido-io/ethconnect/pkg/kldutils"
	uuid "github.com/nu7hatch/gouuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// KafkaBridge receives messages from Kafka and dispatches them to go-ethereum over JSON/RPC
type KafkaBridge struct {
	factory kafkaFactory
	Conf    struct {
		Brokers            []string
		ClientID           string
		ConsumerGroup      string
		InsecureSkipVerify bool
		TopicIn            string
		TopicOut           string
		RPC                struct {
			URL string
		}
		SASL struct {
			Username string
			Password string
		}
		TLS struct {
			ClientCertsFile string
			CACertsFile     string
			Enabled         bool
			PrivateKeyFile  string
		}
	}
	rpc          *rpc.Client
	client       kafkaClient
	signals      chan os.Signal
	consumer     kafkaConsumer
	producer     kafkaProducer
	saramaLogger saramaLogger
}

// NewKafkaBridge creates a new KafkaBridge
func NewKafkaBridge() *KafkaBridge {
	var kf saramaKafkaFactory
	k := KafkaBridge{factory: kf}
	return &k
}

// CobraInit retruns a cobra command to configure this Kafka
func (k *KafkaBridge) CobraInit() (cmd *cobra.Command) {
	cmd = &cobra.Command{
		Use:   "kafka",
		Short: "Kafka bridge to Ethereum",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			err = k.Start()
			return
		},
		PreRunE: func(cmd *cobra.Command, args []string) (err error) {
			if k.Conf.TopicOut == "" {
				return fmt.Errorf("No output topic specified for bridge to send events to")
			}
			if k.Conf.TopicIn == "" {
				return fmt.Errorf("No input topic specified for bridge to listen to")
			}
			if k.Conf.ConsumerGroup == "" {
				return fmt.Errorf("No consumer group specified")
			}
			if err = kldutils.AllOrNoneReqd(cmd, "tls-clientcerts", "tls-clientkey"); err != nil {
				return
			}
			if err = kldutils.AllOrNoneReqd(cmd, "sasl-username", "sasl-password"); err != nil {
				return
			}
			if k.Conf.RPC.URL == "" {
				return fmt.Errorf("No JSON/RPC URL set for ethereum node")
			}
			return
		},
	}
	defBrokerList := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	defTLSenabled, _ := strconv.ParseBool(os.Getenv("KAFKA_TLS_ENABLED"))
	defTLSinsecure, _ := strconv.ParseBool(os.Getenv("KAFKA_TLS_INSECURE"))
	cmd.Flags().StringArrayVarP(&k.Conf.Brokers, "brokers", "b", defBrokerList, "Comma-separated list of bootstrap brokers")
	cmd.Flags().StringVarP(&k.Conf.ClientID, "clientid", "i", os.Getenv("KAFKA_CLIENT_ID"), "Client ID (or generated UUID)")
	cmd.Flags().StringVarP(&k.Conf.ConsumerGroup, "consumer-group", "g", os.Getenv("KAFKA_CONSUMER_GROUP"), "Client ID (or generated UUID)")
	cmd.Flags().StringVarP(&k.Conf.RPC.URL, "rpcurl", "r", os.Getenv("ETH_RPC_URL"), "JSON/RPC URL for Ethereum node")
	cmd.Flags().StringVarP(&k.Conf.TopicIn, "topic-in", "t", os.Getenv("KAFKA_TOPIC_IN"), "Topic to listen to")
	cmd.Flags().StringVarP(&k.Conf.TopicOut, "topic-out", "T", os.Getenv("KAFKA_TOPIC_OUT"), "Topic to send events to")
	cmd.Flags().StringVarP(&k.Conf.TLS.ClientCertsFile, "tls-clientcerts", "c", os.Getenv("KAFKA_TLS_CLIENT_CERT"), "A client certificate file, for mutual TLS auth")
	cmd.Flags().StringVarP(&k.Conf.TLS.PrivateKeyFile, "tls-clientkey", "k", os.Getenv("KAFKA_TLS_CLIENT_KEY"), "A client private key file, for mutual TLS auth")
	cmd.Flags().StringVarP(&k.Conf.TLS.CACertsFile, "tls-cacerts", "C", os.Getenv("KAFKA_TLS_CA_CERTS"), "CA certificates file (or host CAs will be used)")
	cmd.Flags().BoolVarP(&k.Conf.TLS.Enabled, "tls-enabled", "e", defTLSenabled, "Encrypt network connection with TLS (SSL)")
	cmd.Flags().BoolVarP(&k.Conf.InsecureSkipVerify, "tls-insecure", "z", defTLSinsecure, "Disable verification of TLS certificate chain")
	cmd.Flags().StringVarP(&k.Conf.SASL.Username, "sasl-username", "u", os.Getenv("KAFKA_SASL_USERNAME"), "Username for SASL authentication")
	cmd.Flags().StringVarP(&k.Conf.SASL.Password, "sasl-password", "p", os.Getenv("KAFKA_SASL_PASSWORD"), "Password for SASL authentication")
	return
}

func (k *KafkaBridge) createTLSConfiguration() (t *tls.Config, err error) {

	mutualAuth := k.Conf.TLS.ClientCertsFile != "" && k.Conf.TLS.PrivateKeyFile != ""
	log.Debugf("Kafka TLS Enabled=%t Insecure=%t MutualAuth=%t ClientCertsFile=%s PrivateKeyFile=%s CACertsFile=%s",
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

type saramaLogger struct {
}

func (s saramaLogger) Print(v ...interface{}) {
	v = append([]interface{}{"[sarama] "}, v...)
	log.Debug(v...)
}

func (s saramaLogger) Printf(format string, v ...interface{}) {
	log.Debugf("[sarama] "+format, v...)
}

func (s saramaLogger) Println(v ...interface{}) {
	v = append([]interface{}{"[sarama] "}, v...)
	log.Debug(v...)
}

func (k *KafkaBridge) connect() (err error) {

	sarama.Logger = k.saramaLogger
	clientConf := cluster.NewConfig()

	var tlsConfig *tls.Config
	if tlsConfig, err = k.createTLSConfiguration(); err != nil {
		return
	}

	if k.Conf.SASL.Username != "" && k.Conf.SASL.Password != "" {
		clientConf.Net.SASL.Enable = true
		clientConf.Net.SASL.User = k.Conf.SASL.Username
		clientConf.Net.SASL.Password = k.Conf.SASL.Password
	}

	clientConf.Producer.Return.Successes = true
	clientConf.Producer.Return.Errors = true
	clientConf.Producer.RequiredAcks = sarama.WaitForLocal
	clientConf.Producer.Flush.Frequency = 500 * time.Millisecond
	clientConf.Consumer.Return.Errors = true
	clientConf.Group.Return.Notifications = true
	clientConf.Net.TLS.Enable = (tlsConfig != nil)
	clientConf.Net.TLS.Config = tlsConfig
	clientConf.ClientID = k.Conf.ClientID
	if clientConf.ClientID == "" {
		var uuidV4 *uuid.UUID
		if uuidV4, err = uuid.NewV4(); err != nil {
			log.Errorf("Failed to generate UUID for ClientID: %s", err)
			return
		}
		clientConf.ClientID = uuidV4.String()
	}
	log.Debugf("Kafka ClientID: %s", clientConf.ClientID)

	log.Debugf("Kafka Bootstrap brokers: %s", k.Conf.Brokers)
	if k.client, err = k.factory.newClient(k, clientConf); err != nil {
		log.Errorf("Failed to create Kafka client: %s", err)
		return
	}
	var brokers []string
	for _, broker := range k.client.Brokers() {
		brokers = append(brokers, broker.Addr())
	}
	log.Infof("Kafka Connected: %s", brokers)

	// Connect the client
	if k.rpc, err = rpc.Dial(k.Conf.RPC.URL); err != nil {
		err = fmt.Errorf("JSON/RPC connection to %s failed: %s", k.Conf.RPC.URL, err)
		return
	}
	log.Debug("JSON/RPC connected. URL=", k.Conf.RPC.URL)

	return
}

func (k *KafkaBridge) startProducer() (err error) {

	log.Debugf("Kafka Producer Topic=%s", k.Conf.TopicOut)
	if k.producer, err = k.client.newProducer(k); err != nil {
		log.Errorf("Failed to create Kafka producer: %s", err)
		return
	}

	go func() {
		for err := range k.producer.Errors() {
			log.Error("Kafka producer failed:", err)
		}
	}()
	go func() {
		for msg := range k.producer.Successes() {
			log.Debugf("Kafka producer sent message. Partition=%d Offset=%d", msg.Partition, msg.Offset)
		}
	}()

	log.Infof("Kafka Created producer")
	return
}

func (k *KafkaBridge) startConsumer() (err error) {

	log.Debugf("Kafka Consumer Topic=%s ConsumerGroup=%s", k.Conf.TopicIn, k.Conf.ConsumerGroup)
	if k.consumer, err = k.client.newConsumer(k); err != nil {
		log.Errorf("Failed to create Kafka consumer: %s", err)
		return
	}

	go func() {
		for err := range k.consumer.Errors() {
			log.Error("Kafka consumer failed:", err)
		}
	}()
	go func() {
		for ntf := range k.consumer.Notifications() {
			log.Debugf("Kafka consumer rebalanced. Current=%+v", ntf.Current)
		}
	}()
	go func() {
		for msg := range k.consumer.Messages() {
			log.Debugf("Kafka consumer received message")
			k.onMessage(msg.Value)
		}
	}()

	log.Infof("Kafka Created consumer")
	return
}

func (k *KafkaBridge) onMessage(msg []byte) {

	var commonMsg kldmessages.MessageCommon
	if err := json.Unmarshal(msg, &commonMsg); err != nil {
		log.Errorf("Failed to unmarshal headers from incoming message '%s': %s", hex.Dump(msg), err)
		return
	}

	if commonMsg.Headers.MsgType == kldmessages.MsgTypeDeployContract {
		k.processDeployContract(msg)
	} else {
		log.Errorf("Unknown message type '%s' in message '%s'", commonMsg.Headers.MsgType, hex.Dump(msg))
		return
	}

}

func (k *KafkaBridge) processDeployContract(msg []byte) {
	var deployContractMsg kldmessages.DeployContract
	if err := json.Unmarshal(msg, &deployContractMsg); err != nil {
		log.Errorf("Failed to unmarshal headers from DeployContract message '%s': %s", hex.Dump(msg), err)
		return
	}

	kldTx, err := kldeth.NewContractDeployTxn(deployContractMsg)
	if err != nil {
		log.Errorf("Failed to parse DeployContract message '%s': %s", hex.Dump(msg), err)
		return
	}

	txHash, err := kldTx.Send(k.rpc)
	if err != nil {
		log.Errorf("Contract deployment failed: %s", err)
		return
	}
	log.Infof("Sent contract creation transaction successfully: %s", txHash)
}

// Start kicks off the bridge
func (k *KafkaBridge) Start() (err error) {

	if err = k.connect(); err != nil {
		return
	}
	if err = k.startConsumer(); err != nil {
		return
	}
	if err = k.startProducer(); err != nil {
		return
	}

	k.signals = make(chan os.Signal, 1)
	signal.Notify(k.signals, os.Interrupt)
	for {
		select {
		case <-k.signals:
			k.producer.Close()
			k.consumer.Close()

			log.Infof("Kafka Bridge complete")
			return
		}
	}
}
