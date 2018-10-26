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
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	cluster "github.com/bsm/sarama-cluster"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// KafkaCommonConf - Common configuration for Kafka
type KafkaCommonConf struct {
	Brokers       []string `json:"brokers"`
	ClientID      string   `json:"clientID"`
	ConsumerGroup string   `json:"consumerGroup"`
	TopicIn       string   `json:"topicIn"`
	TopicOut      string   `json:"topicOut"`
	SASL          struct {
		Username string
		Password string
	} `json:"sasl"`
	TLS kldutils.TLSConfig `json:"tls"`
}

// KafkaCommon is the base interface for bridges that interact with Kafka
type KafkaCommon interface {
	ValidateConf() error
	CobraInit(cmd *cobra.Command)
	Start() error
	Conf() *KafkaCommonConf
	Producer() KafkaProducer
}

// NewKafkaCommon constructs a new KafkaCommon instance
func NewKafkaCommon(kf KafkaFactory, conf *KafkaCommonConf, kafkaGoRoutines KafkaGoRoutines) (k KafkaCommon) {
	k = &kafkaCommon{
		factory:         kf,
		kafkaGoRoutines: kafkaGoRoutines,
		conf:            conf,
	}
	return
}

// *kafkaCommon provides a base command for establishing Kafka connectivity with a
// producer and a consumer-group
type kafkaCommon struct {
	conf            *KafkaCommonConf
	factory         KafkaFactory
	rpc             *rpc.Client
	client          KafkaClient
	signals         chan os.Signal
	consumer        KafkaConsumer
	consumerWG      sync.WaitGroup
	producer        KafkaProducer
	producerWG      sync.WaitGroup
	kafkaGoRoutines KafkaGoRoutines
	saramaLogger    saramaLogger
}

func (k *kafkaCommon) Conf() *KafkaCommonConf {
	return k.conf
}

func (k *kafkaCommon) Producer() KafkaProducer {
	return k.producer
}

// ValidateConf performs common Cobra PreRunE logic for Kafka related commands
func (k *kafkaCommon) ValidateConf() (err error) {
	if k.conf.TopicOut == "" {
		return fmt.Errorf("No output topic specified for bridge to send events to")
	}
	if k.conf.TopicIn == "" {
		return fmt.Errorf("No input topic specified for bridge to listen to")
	}
	if k.conf.ConsumerGroup == "" {
		return fmt.Errorf("No consumer group specified")
	}
	if !kldutils.AllOrNoneReqd(k.conf.SASL.Username, k.conf.SASL.Password) {
		err = fmt.Errorf("Username and Password must both be provided for SASL")
		return
	}
	return
}

// CobraInit performs common Cobra init for Kafka related commands
func (k *kafkaCommon) CobraInit(cmd *cobra.Command) {
	defBrokerList := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	defTLSenabled, _ := strconv.ParseBool(os.Getenv("KAFKA_TLS_ENABLED"))
	defTLSinsecure, _ := strconv.ParseBool(os.Getenv("KAFKA_TLS_INSECURE"))
	cmd.Flags().StringArrayVarP(&k.conf.Brokers, "brokers", "b", defBrokerList, "Comma-separated list of bootstrap brokers")
	cmd.Flags().StringVarP(&k.conf.ClientID, "clientid", "i", os.Getenv("KAFKA_CLIENT_ID"), "Client ID (or generated UUID)")
	cmd.Flags().StringVarP(&k.conf.ConsumerGroup, "consumer-group", "g", os.Getenv("KAFKA_CONSUMER_GROUP"), "Client ID (or generated UUID)")
	cmd.Flags().StringVarP(&k.conf.TopicIn, "topic-in", "t", os.Getenv("KAFKA_TOPIC_IN"), "Topic to listen to")
	cmd.Flags().StringVarP(&k.conf.TopicOut, "topic-out", "T", os.Getenv("KAFKA_TOPIC_OUT"), "Topic to send events to")
	cmd.Flags().StringVarP(&k.conf.TLS.ClientCertsFile, "tls-clientcerts", "c", os.Getenv("KAFKA_TLS_CLIENT_CERT"), "A client certificate file, for mutual TLS auth")
	cmd.Flags().StringVarP(&k.conf.TLS.ClientKeyFile, "tls-clientkey", "k", os.Getenv("KAFKA_TLS_CLIENT_KEY"), "A client private key file, for mutual TLS auth")
	cmd.Flags().StringVarP(&k.conf.TLS.CACertsFile, "tls-cacerts", "C", os.Getenv("KAFKA_TLS_CA_CERTS"), "CA certificates file (or host CAs will be used)")
	cmd.Flags().BoolVarP(&k.conf.TLS.Enabled, "tls-enabled", "e", defTLSenabled, "Encrypt network connection with TLS (SSL)")
	cmd.Flags().BoolVarP(&k.conf.TLS.InsecureSkipVerify, "tls-insecure", "z", defTLSinsecure, "Disable verification of TLS certificate chain")
	cmd.Flags().StringVarP(&k.conf.SASL.Username, "sasl-username", "u", os.Getenv("KAFKA_SASL_USERNAME"), "Username for SASL authentication")
	cmd.Flags().StringVarP(&k.conf.SASL.Password, "sasl-password", "p", os.Getenv("KAFKA_SASL_PASSWORD"), "Password for SASL authentication")
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

func (k *kafkaCommon) connect() (err error) {

	sarama.Logger = k.saramaLogger
	clientConf := cluster.NewConfig()

	var tlsConfig *tls.Config
	if tlsConfig, err = kldutils.CreateTLSConfiguration(&k.conf.TLS); err != nil {
		return
	}

	if k.conf.SASL.Username != "" && k.conf.SASL.Password != "" {
		clientConf.Net.SASL.Enable = true
		clientConf.Net.SASL.User = k.conf.SASL.Username
		clientConf.Net.SASL.Password = k.conf.SASL.Password
	}

	clientConf.Producer.Return.Successes = true
	clientConf.Producer.Return.Errors = true
	clientConf.Producer.RequiredAcks = sarama.WaitForLocal
	clientConf.Producer.Flush.Frequency = 500 * time.Millisecond
	clientConf.Metadata.Retry.Backoff = 2 * time.Second
	clientConf.Consumer.Return.Errors = true
	clientConf.Group.Return.Notifications = true
	clientConf.Net.TLS.Enable = (tlsConfig != nil)
	clientConf.Net.TLS.Config = tlsConfig
	clientConf.ClientID = k.conf.ClientID
	if clientConf.ClientID == "" {
		clientConf.ClientID = kldutils.UUIDv4()
	}
	log.Debugf("Kafka ClientID: %s", clientConf.ClientID)

	log.Debugf("Kafka Bootstrap brokers: %s", k.conf.Brokers)
	if k.client, err = k.factory.NewClient(k, clientConf); err != nil {
		log.Errorf("Failed to create Kafka client: %s", err)
		return
	}
	var brokers []string
	for _, broker := range k.client.Brokers() {
		brokers = append(brokers, broker.Addr())
	}
	log.Infof("Kafka Connected: %s", brokers)

	return
}

func (k *kafkaCommon) createProducer() (err error) {
	log.Debugf("Kafka Producer Topic=%s", k.conf.TopicOut)
	if k.producer, err = k.client.NewProducer(k); err != nil {
		log.Errorf("Failed to create Kafka producer: %s", err)
		return
	}
	return
}

func (k *kafkaCommon) startProducer() (err error) {

	k.producerWG.Add(2)

	go k.kafkaGoRoutines.ProducerErrorLoop(k.consumer, k.producer, &k.producerWG)

	go k.kafkaGoRoutines.ProducerSuccessLoop(k.consumer, k.producer, &k.producerWG)

	log.Infof("Kafka Created producer")
	return
}

func (k *kafkaCommon) createConsumer() (err error) {
	log.Debugf("Kafka Consumer Topic=%s ConsumerGroup=%s", k.conf.TopicIn, k.conf.ConsumerGroup)
	if k.consumer, err = k.client.NewConsumer(k); err != nil {
		log.Errorf("Failed to create Kafka consumer: %s", err)
		return
	}
	return
}

func (k *kafkaCommon) startConsumer() (err error) {

	k.consumerWG.Add(3)
	go func() {
		for err := range k.consumer.Errors() {
			log.Error("Kafka consumer failed:", err)
		}
		k.consumerWG.Done()
	}()
	go func() {
		for ntf := range k.consumer.Notifications() {
			log.Debugf("Kafka consumer rebalanced. Current=%+v", ntf.Current)
		}
		k.consumerWG.Done()
	}()
	go k.kafkaGoRoutines.ConsumerMessagesLoop(k.consumer, k.producer, &k.consumerWG)

	log.Infof("Kafka Created consumer")
	return
}

// Start kicks off the bridge
func (k *kafkaCommon) Start() (err error) {

	if err = k.connect(); err != nil {
		return
	}
	if err = k.createConsumer(); err != nil {
		return
	}
	if err = k.createProducer(); err != nil {
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
			k.producer.AsyncClose()
			k.consumer.Close()
			k.producerWG.Wait()
			k.consumerWG.Wait()

			log.Infof("Kafka Bridge complete")
			return
		}
	}
}
