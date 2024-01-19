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

package kafka

import (
	"crypto/tls"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/hyperledger/firefly-ethconnect/internal/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	DefaultSendRetryDelay = 5 * time.Second
)

// KafkaCommonConf - Common configuration for Kafka
type KafkaCommonConf struct {
	Brokers          []string `json:"brokers"`
	ClientID         string   `json:"clientID"`
	ConsumerGroup    string   `json:"consumerGroup"`
	TopicIn          string   `json:"topicIn"`
	TopicOut         string   `json:"topicOut"`
	SendRetryDelayMS int      `json:"sendRetryDelayMS"`
	ProducerFlush    struct {
		Frequency int `json:"frequency"`
		Messages  int `json:"messages"`
		Bytes     int `json:"bytes"`
	} `json:"producerFlush"`
	SASL struct {
		Username string
		Password string
	} `json:"sasl"`
	TLS utils.TLSConfig `json:"tls"`

	// Computed
	sendRetryDelay time.Duration
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
	conf.sendRetryDelay = time.Duration(conf.SendRetryDelayMS) * time.Millisecond
	if conf.sendRetryDelay <= 0 {
		conf.sendRetryDelay = DefaultSendRetryDelay
	}
	return
}

// *kafkaCommon provides a base command for establishing Kafka connectivity with a
// producer and a consumer-group
type kafkaCommon struct {
	conf            *KafkaCommonConf
	factory         KafkaFactory
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
func (k *kafkaCommon) ValidateConf() error {
	return KafkaValidateConf(k.conf)
}

// KafkaValidateConf validates supplied configuration
func KafkaValidateConf(kconf *KafkaCommonConf) (err error) {
	if kconf.TopicOut == "" {
		return errors.Errorf(errors.ConfigKafkaMissingOutputTopic)
	}
	if kconf.TopicIn == "" {
		return errors.Errorf(errors.ConfigKafkaMissingInputTopic)
	}
	if kconf.ConsumerGroup == "" {
		return errors.Errorf(errors.ConfigKafkaMissingConsumerGroup)
	}
	if !utils.AllOrNoneReqd(kconf.SASL.Username, kconf.SASL.Password) {
		err = errors.Errorf(errors.ConfigKafkaMissingBadSASL)
		return
	}
	return
}

// CobraInit performs common Cobra init for Kafka related commands
func (k *kafkaCommon) CobraInit(cmd *cobra.Command) {
	KafkaCommonCobraInit(cmd, k.conf)
}

// KafkaCommonCobraInit commandline common parameter init for Kafka
func KafkaCommonCobraInit(cmd *cobra.Command, kconf *KafkaCommonConf) {
	defBrokerList := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	if len(defBrokerList) == 1 && defBrokerList[0] == "" {
		defBrokerList = []string{}
	}
	defTLSenabled, _ := strconv.ParseBool(os.Getenv("KAFKA_TLS_ENABLED"))
	defTLSinsecure, _ := strconv.ParseBool(os.Getenv("KAFKA_TLS_INSECURE"))
	cmd.Flags().StringArrayVarP(&kconf.Brokers, "brokers", "b", defBrokerList, "Comma-separated list of bootstrap brokers")
	cmd.Flags().StringVarP(&kconf.ClientID, "clientid", "i", os.Getenv("KAFKA_CLIENT_ID"), "Client ID (or generated UUID)")
	cmd.Flags().StringVarP(&kconf.ConsumerGroup, "consumer-group", "g", os.Getenv("KAFKA_CONSUMER_GROUP"), "Client ID (or generated UUID)")
	cmd.Flags().StringVarP(&kconf.TopicIn, "topic-in", "t", os.Getenv("KAFKA_TOPIC_IN"), "Topic to listen to")
	cmd.Flags().StringVarP(&kconf.TopicOut, "topic-out", "T", os.Getenv("KAFKA_TOPIC_OUT"), "Topic to send events to")
	cmd.Flags().StringVarP(&kconf.TLS.ClientCertsFile, "tls-clientcerts", "c", os.Getenv("KAFKA_TLS_CLIENT_CERT"), "A client certificate file, for mutual TLS auth")
	cmd.Flags().StringVarP(&kconf.TLS.ClientKeyFile, "tls-clientkey", "k", os.Getenv("KAFKA_TLS_CLIENT_KEY"), "A client private key file, for mutual TLS auth")
	cmd.Flags().StringVarP(&kconf.TLS.CACertsFile, "tls-cacerts", "C", os.Getenv("KAFKA_TLS_CA_CERTS"), "CA certificates file (or host CAs will be used)")
	cmd.Flags().BoolVarP(&kconf.TLS.Enabled, "tls-enabled", "e", defTLSenabled, "Encrypt network connection with TLS (SSL)")
	cmd.Flags().BoolVarP(&kconf.TLS.InsecureSkipVerify, "tls-insecure", "z", defTLSinsecure, "Disable verification of TLS certificate chain")
	cmd.Flags().StringVarP(&kconf.SASL.Username, "sasl-username", "u", os.Getenv("KAFKA_SASL_USERNAME"), "Username for SASL authentication")
	cmd.Flags().StringVarP(&kconf.SASL.Password, "sasl-password", "p", os.Getenv("KAFKA_SASL_PASSWORD"), "Password for SASL authentication")
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

func getFetchDefault() int32 {
	fetchDefault := int32(1024 * 1024)
	cb := GetCircuitBreaker()
	if cb != nil {
		// Default fetch should not be more than 5% of the configured circuit breaker buffer.
		// The cicruit breaker requires a fetch to occur, to update the high water mark.
		// So if we hardly ever fetch, then there's a higher chance the producer could
		// run over the buffer without us switching the switch
		fivePercent := int32(cb.conf.UpperBound / 20)
		if fivePercent > 0 && fivePercent < fetchDefault {
			fetchDefault = fivePercent
		}
	}
	return fetchDefault
}

func (k *kafkaCommon) connect() (err error) {

	log.Debugf("Kafka Bootstrap brokers: %s", k.conf.Brokers)
	if len(k.conf.Brokers) == 0 || k.conf.Brokers[0] == "" {
		err = errors.Errorf(errors.ConfigKafkaMissingBrokers)
		return
	}

	sarama.Logger = k.saramaLogger
	clientConf := sarama.NewConfig()

	var tlsConfig *tls.Config
	if tlsConfig, err = utils.CreateTLSConfiguration(&k.conf.TLS); err != nil {
		return
	}

	if k.conf.SASL.Username != "" && k.conf.SASL.Password != "" {
		clientConf.Net.SASL.Enable = true
		clientConf.Net.SASL.User = k.conf.SASL.Username
		clientConf.Net.SASL.Password = k.conf.SASL.Password
	}

	clientConf.Consumer.Fetch.Default = getFetchDefault()

	clientConf.Producer.Return.Successes = true
	clientConf.Producer.Return.Errors = true
	clientConf.Producer.RequiredAcks = sarama.WaitForLocal
	clientConf.Producer.Flush.Frequency = time.Duration(k.conf.ProducerFlush.Frequency) * time.Millisecond
	clientConf.Producer.Flush.Messages = k.conf.ProducerFlush.Messages
	clientConf.Producer.Flush.Bytes = k.conf.ProducerFlush.Bytes
	clientConf.Metadata.Retry.Backoff = 2 * time.Second
	clientConf.Consumer.Return.Errors = true
	clientConf.Version = sarama.V2_0_0_0
	clientConf.Net.TLS.Enable = (tlsConfig != nil)
	clientConf.Net.TLS.Config = tlsConfig
	clientConf.ClientID = k.conf.ClientID
	if clientConf.ClientID == "" {
		clientConf.ClientID = utils.UUIDv4()
	}
	log.Debugf("Kafka ClientID: %s", clientConf.ClientID)

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

	k.consumerWG.Add(2) // messages and errors
	go func() {
		for err := range k.consumer.Errors() {
			log.Error("Kafka consumer failed:", err)
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

	log.Debugf("Kafka initialization complete")
	k.signals = make(chan os.Signal, 1)
	signal.Notify(k.signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	for s := range k.signals {
		k.producer.AsyncClose()
		k.consumer.Close()
		k.producerWG.Wait()
		k.consumerWG.Wait()

		log.Infof("Kafka Bridge complete (sig=%s)", s.String())
		return
	}
	return
}
