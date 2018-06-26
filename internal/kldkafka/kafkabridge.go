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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	cluster "github.com/bsm/sarama-cluster"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// KafkaBridge receives messages from Kafka and dispatches them to go-ethereum over JSON/RPC
type KafkaBridge struct {
	Conf struct {
		Brokers            []string
		ClientID           string
		ConsumerGroup      string
		InsecureSkipVerify bool
		MaxInFlight        int
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
	factory      kafkaFactory
	rpc          *rpc.Client
	client       kafkaClient
	signals      chan os.Signal
	consumer     kafkaConsumer
	consumerWG   sync.WaitGroup
	producer     kafkaProducer
	producerWG   sync.WaitGroup
	saramaLogger saramaLogger
	processor    MsgProcessor
	inFlight     map[string]*msgContext
	inFlightCond *sync.Cond
}

func defInt(envVarName string, defValue int) int {
	defStr := os.Getenv(envVarName)
	if defStr == "" {
		return defValue
	}
	parsedInt, err := strconv.ParseInt(defStr, 10, 32)
	if err != nil {
		log.Errorf("Invalid string in env var %s", envVarName)
		return defValue
	}
	return int(parsedInt)
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
	cmd.Flags().IntVarP(&k.Conf.MaxInFlight, "maxinflight", "m", defInt("KAFKA_MAX_INFLIGHT", 10), "Maximum messages to hold in-flight")
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

// MsgContext is passed for each message that arrives at the bridge
type MsgContext interface {
	// Get the headers of the message
	Headers() *kldmessages.CommonHeaders
	// Unmarshal the supplied message into a give type
	Unmarshal(msg interface{}) error
	// Send a reply that can be marshaled into bytes.
	// Sets all the common headers on behalf of the caller, based on the request context
	Reply(fullMsg interface{}) error
}

type msgContext struct {
	timeReceived   time.Time
	requestCommon  kldmessages.RequestCommon
	origMsg        string
	saramaMsg      *sarama.ConsumerMessage
	key            string
	bridge         *KafkaBridge
	complete       bool
	replyType      string
	replyTime      time.Time
	replyBytes     []byte
	replyPartition int32
	replyOffset    int64
}

// addInflightMsg creates a msgContext wrapper around a message with all the
// relevant context, and adds it to the inFlight map
// * Caller holds the inFlightCond mutex, and has already checked for capacity *
func (k *KafkaBridge) addInflightMsg(msg *sarama.ConsumerMessage) (pCtx *msgContext, err error) {
	ctx := msgContext{
		timeReceived: time.Now(),
		origMsg:      fmt.Sprintf("%s:%d:%d", k.Conf.TopicIn, msg.Partition, msg.Offset),
		saramaMsg:    msg,
		bridge:       k,
	}
	// Add it to our inflight map - from this point on we need to ensure we remove it, to avoid leaks.
	// Messages are only removed from the inflight map when a response is sent, so it
	// is very important that the consumer of the wrapped context object calls Reply
	pCtx = &ctx
	k.inFlight[ctx.origMsg] = pCtx
	log.Infof("Message now in-flight: %s", pCtx)
	// Attempt to process the headers from the original message,
	// which could fail. In which case we still have a msgContext inflight
	// that needs Reply (and offset commit). So our caller must
	// send a generic error reply (after dropping the lock).
	if err = json.Unmarshal(msg.Value, &ctx.requestCommon); err != nil {
		log.Errorf("Failed to unmarshal message headers: %s", err)
		return
	}
	headers := &ctx.requestCommon.Headers
	if headers.ID == "" {
		headers.ID = kldutils.UUIDv4()
	}
	// Use the account as the partitioning key, or fallback to the ID, which we ensure is non-null
	if headers.Account != "" {
		ctx.key = headers.Account
	} else {
		ctx.key = headers.ID
	}
	return
}

type ctxByOffset []*msgContext

func (a ctxByOffset) Len() int {
	return len(a)
}
func (a ctxByOffset) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}
func (a ctxByOffset) Less(i, j int) bool {
	return a[i].saramaMsg.Offset < a[j].saramaMsg.Offset
}

// Mark that a currently in-flight context is now ready.
// Looks at the other in-flight messages for the same partition, and works out if
// we can move the offset forwards.
// * Caller holds the inFlightCond mutex *
func (k *KafkaBridge) setInFlightComplete(ctx *msgContext) (err error) {

	// Build an offset sorted list of the inflight
	ctx.complete = true
	var completeInParition []*msgContext
	for _, inflight := range k.inFlight {
		if inflight.saramaMsg.Partition == ctx.saramaMsg.Partition {
			completeInParition = append(completeInParition, inflight)
		}
	}
	sort.Sort(ctxByOffset(completeInParition))

	// Go forwards until the first that isn't complete
	var readyToAck []*msgContext
	for i := 0; i < len(completeInParition); i++ {
		if completeInParition[i].complete {
			readyToAck = append(readyToAck, completeInParition[i])
		} else {
			break
		}
	}

	canMark := len(readyToAck) > 0
	log.Debugf("Ready=%d:%d CanMark=%t Infight=%d InflightSamePartition=%d ReadyToAck=%d",
		ctx.saramaMsg.Partition, ctx.saramaMsg.Offset, canMark,
		len(k.inFlight), len(completeInParition), len(readyToAck))
	if canMark {
		// Remove all the ready-to-acks from the in-flight list
		for i := 0; i < len(readyToAck); i++ {
			delete(k.inFlight, readyToAck[i].origMsg)
		}
		// Update the offset
		highestOffset := readyToAck[len(readyToAck)-1].saramaMsg
		log.Infof("Marking offset %d:%d", highestOffset.Offset, highestOffset.Partition)
		k.consumer.MarkOffset(highestOffset, "")
	}

	return
}

func (c *msgContext) Headers() *kldmessages.CommonHeaders {
	return &c.requestCommon.Headers
}

func (c *msgContext) Unmarshal(msg interface{}) error {
	return json.Unmarshal(c.saramaMsg.Value, msg)
}

func (c *msgContext) Reply(pFullMsg interface{}) (err error) {

	replyHeaders := &pFullMsg.(*kldmessages.ReplyCommon).Headers
	c.replyType = replyHeaders.MsgType
	replyHeaders.ID = kldutils.UUIDv4()
	replyHeaders.Context = c.requestCommon.Headers.Context
	replyHeaders.OrigID = c.requestCommon.Headers.ID
	replyHeaders.OrigMsg = c.origMsg
	if c.replyBytes, err = json.Marshal(pFullMsg); err != nil {
		return
	}
	log.Infof("Sending reply: %s", c)
	c.bridge.producer.Input() <- &sarama.ProducerMessage{
		Topic:    c.bridge.Conf.TopicOut,
		Key:      sarama.StringEncoder(c.key),
		Metadata: c.origMsg,
		Value:    c,
	}
	return
}

func (c *msgContext) String() string {
	return fmt.Sprintf("MsgContext[%s:%s origMsg=%s received=%s replied=%s replyType=%s]",
		c.requestCommon.Headers.MsgType, c.requestCommon.Headers.ID,
		c.origMsg, c.timeReceived.Format(time.RFC3339),
		c.replyTime.Format(time.RFC3339), c.replyType)
}

// Length Gets the encoded length
func (c msgContext) Length() int {
	return len(c.replyBytes)
}

// Encode Does the encoding
func (c msgContext) Encode() ([]byte, error) {
	return c.replyBytes, nil
}

// NewKafkaBridge creates a new KafkaBridge
func NewKafkaBridge() *KafkaBridge {
	var kf saramaKafkaFactory
	var mp msgProcessor
	k := KafkaBridge{
		factory:      &kf,
		processor:    &mp,
		inFlight:     make(map[string]*msgContext),
		inFlightCond: sync.NewCond(&sync.Mutex{}),
	}
	return &k
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
		clientConf.ClientID = kldutils.UUIDv4()
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

	k.producerWG.Add(2)

	go func() {
		for err := range k.producer.Errors() {
			k.inFlightCond.L.Lock()
			// If we fail to send a reply, this is significant. We have a request in flight
			// and we have probably already sent the message.
			// Currently we panic, on the basis that we will be restarted by Docker
			// to drive retry logic. In the future we might consider recreating the
			// producer and attempting to resend the message a number of times -
			// keeping a retry counter on the msgContext object
			origMsg := err.Msg.Metadata.(string)
			ctx := k.inFlight[origMsg]
			log.Errorf("Kafka producer failed for reply %s to origMsg %s: %s", ctx, origMsg, err)
			panic(err)
			// k.inFlightCond.L.Unlock() - unreachable while we have a panic
		}
		k.producerWG.Done()
	}()

	go func() {
		for msg := range k.producer.Successes() {
			k.inFlightCond.L.Lock()
			origMsg := msg.Metadata.(string)
			if ctx, ok := k.inFlight[origMsg]; ok {
				log.Infof("Reply sent: %s", ctx)
				// While still holding the lock, add this to the completed list
				k.setInFlightComplete(ctx)
				// We've reduced the in-flight count - wake any waiting consumer go func
				k.inFlightCond.Broadcast()
			} else {
				// This should never happen. Represents a logic bug that must be diagnosed.
				log.Errorf("Received confirmation for message not in in-flight map: %s", origMsg)
				panic(err)
			}
			k.inFlightCond.L.Unlock()
		}
		k.producerWG.Done()
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
	go func() {
		for msg := range k.consumer.Messages() {
			k.inFlightCond.L.Lock()
			log.Infof("Kafka consumer received message: Partition=%d Offset=%d", msg.Partition, msg.Offset)

			// We cannot build up an infinite number of messages in memory
			for len(k.inFlight) >= k.Conf.MaxInFlight {
				log.Infof("Too many messages in-flight: In-flight=%d Max=%d", len(k.inFlight), k.Conf.MaxInFlight)
				k.inFlightCond.Wait()
			}
			// addInflightMsg always adds the message, even if it cannot
			// be parsed
			msgCtx, err := k.addInflightMsg(msg)
			// Unlock before any further processing
			k.inFlightCond.L.Unlock()
			if err == nil {
				// Dispatch for processing if we parsed the message successfully
				k.processor.OnMessage(msgCtx)
			} else {
				// Dispatch a generic 'bad data' reply
				var errMsg kldmessages.ReplyCommon
				errMsg.Headers.Status = 400
				errMsg.Headers.ErrorMessage = err.Error()
				msgCtx.Reply(&errMsg)
			}
		}
		k.consumerWG.Done()
	}()

	log.Infof("Kafka Created consumer")
	return
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
			k.producer.AsyncClose()
			k.consumer.Close()
			k.producerWG.Wait()
			k.consumerWG.Wait()

			log.Infof("Kafka Bridge complete")
			return
		}
	}
}
