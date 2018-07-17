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

package kldwebhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/Shopify/sarama"
	"github.com/icza/dyno"
	"github.com/kaleido-io/ethconnect/internal/kldkafka"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	// MaxHeaderSize max size of content
	MaxHeaderSize = 16 * 1024
	// MaxPayloadSize max size of content
	MaxPayloadSize = 128 * 1024
)

// WebhooksBridgeConf defines the YAML config structure for a webhooks bridge instance
type WebhooksBridgeConf struct {
	Kafka kldkafka.KafkaCommonConf `yaml:"kafka"`
	HTTP  struct {
		LocalAddr string             `yaml:"localAddr"`
		Port      int                `yaml:"port"`
		TLS       kldutils.TLSConfig `yaml:"tls"`
	} `yaml:"http"`
}

// WebhooksBridge receives messages over HTTP POST and sends them to Kafka
type WebhooksBridge struct {
	conf        WebhooksBridgeConf
	kafka       kldkafka.KafkaCommon
	srv         *http.Server
	sendCond    *sync.Cond
	pendingMsgs map[string]bool
	successMsgs map[string]*sarama.ProducerMessage
	failedMsgs  map[string]error
}

// Conf gets the config for this bridge
func (w *WebhooksBridge) Conf() *WebhooksBridgeConf {
	return &w.conf
}

// SetConf sets the config for this bridge
func (w *WebhooksBridge) SetConf(conf *WebhooksBridgeConf) {
	w.conf = *conf
}

// ValidateConf validates the config
func (w *WebhooksBridge) ValidateConf() (err error) {
	// No validation currently
	return
}

// NewWebhooksBridge constructor
func NewWebhooksBridge() (w *WebhooksBridge) {
	w = &WebhooksBridge{
		sendCond:    sync.NewCond(&sync.Mutex{}),
		pendingMsgs: make(map[string]bool),
		successMsgs: make(map[string]*sarama.ProducerMessage),
		failedMsgs:  make(map[string]error),
	}
	kf := &kldkafka.SaramaKafkaFactory{}
	w.kafka = kldkafka.NewKafkaCommon(kf, w)
	return
}

// CobraInit retruns a cobra command to configure this KafkaBridge
func (w *WebhooksBridge) CobraInit() (cmd *cobra.Command) {
	cmd = &cobra.Command{
		Use:   "webhooks",
		Short: "Webhooks->Kafka Bridge",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			err = w.Start()
			return
		},
		PreRunE: func(cmd *cobra.Command, args []string) (err error) {
			if err = w.kafka.ValidateConf(); err != nil {
				return
			}

			// The simple commandline interface requires the TLS configuration for
			// both Kafka and HTTP is the same. Only the YAML configuration allows
			// them to be different
			w.conf.HTTP.TLS = w.kafka.Conf().TLS

			err = w.ValidateConf()
			return
		},
	}
	w.kafka.CobraInit(cmd)
	cmd.Flags().StringVarP(&w.conf.HTTP.LocalAddr, "listen-addr", "L", os.Getenv("WEBHOOKS_LISTEN_ADDR"), "Local address to listen on")
	cmd.Flags().IntVarP(&w.conf.HTTP.Port, "listen-port", "l", kldutils.DefInt("WEBHOOKS_LISTEN_PORT", 8080), "Port to listen on")
	return
}

// ConsumerMessagesLoop - consume replies
func (w *WebhooksBridge) ConsumerMessagesLoop(consumer kldkafka.KafkaConsumer, producer kldkafka.KafkaProducer, wg *sync.WaitGroup) {
	for msg := range consumer.Messages() {
		log.Infof("Webhooks received reply: %s", msg)
	}
	wg.Done()
}

func (w *WebhooksBridge) setMsgPending(msgID string) {
	w.sendCond.L.Lock()
	w.pendingMsgs[msgID] = true
	w.sendCond.L.Unlock()
}

func (w *WebhooksBridge) waitForSend(msgID string) (msg *sarama.ProducerMessage, err error) {
	w.sendCond.L.Lock()
	for msg == nil && err == nil {
		var found bool
		if err, found = w.failedMsgs[msgID]; found {
			delete(w.failedMsgs, msgID)
		} else if msg, found = w.successMsgs[msgID]; found {
			delete(w.successMsgs, msgID)
		} else {
			w.sendCond.Wait()
		}
	}
	w.sendCond.L.Unlock()
	return
}

// ProducerErrorLoop - consume errors
func (w *WebhooksBridge) ProducerErrorLoop(consumer kldkafka.KafkaConsumer, producer kldkafka.KafkaProducer, wg *sync.WaitGroup) {
	log.Debugf("Webhooks listening for errors sending to Kafka")
	for err := range producer.Errors() {
		log.Errorf("Error sending message: %s", err)
		if err.Msg == nil || err.Msg.Metadata == nil {
			// This should not be possible
			panic(fmt.Errorf("Error did not contain message and metadata: %+v", err))
		}
		msgID := err.Msg.Metadata.(string)
		w.sendCond.L.Lock()
		if _, found := w.pendingMsgs[msgID]; found {
			delete(w.pendingMsgs, msgID)
			w.failedMsgs[msgID] = err
			w.sendCond.Broadcast()
		}
		w.sendCond.L.Unlock()
	}
	wg.Done()
}

// ProducerSuccessLoop - consume successes
func (w *WebhooksBridge) ProducerSuccessLoop(consumer kldkafka.KafkaConsumer, producer kldkafka.KafkaProducer, wg *sync.WaitGroup) {
	log.Debugf("Webhooks listening for successful sends to Kafka")
	for msg := range producer.Successes() {
		log.Infof("Webhooks sent message ok: %s", msg)
		if msg.Metadata == nil {
			// This should not be possible
			panic(fmt.Errorf("Sent message did not contain metadata: %+v", msg))
		}
		msgID := msg.Metadata.(string)
		w.sendCond.L.Lock()
		if _, found := w.pendingMsgs[msgID]; found {
			delete(w.pendingMsgs, msgID)
			w.successMsgs[msgID] = msg
			w.sendCond.Broadcast()
		}
		w.sendCond.L.Unlock()
	}
	wg.Done()
}

type errMsg struct {
	Sent    bool   `json:"sent"`
	Message string `json:"error"`
}

func errReply(res http.ResponseWriter, err error, status int) {
	reply, _ := json.Marshal(&errMsg{Message: err.Error()})
	res.WriteHeader(status)
	res.Write(reply)
	return
}

type okMsg struct {
	OK bool `json:"ok"`
}

func okReply(res http.ResponseWriter) {
	reply, _ := json.Marshal(&okMsg{OK: true})
	res.WriteHeader(200)
	res.Write(reply)
	return
}

type sentMsg struct {
	Sent    bool   `json:"sent"`
	Request string `json:"id"`
	Msg     string `json:"msg,omitempty"`
}

func msgSentReply(res http.ResponseWriter, ack bool, msg *sarama.ProducerMessage) {
	msgAck := ""
	if ack {
		msgAck = fmt.Sprintf("%s:%d:%d", msg.Topic, msg.Partition, msg.Offset)
	}
	replyMsg := sentMsg{
		Sent:    true,
		Request: msg.Metadata.(string),
		Msg:     msgAck,
	}
	reply, _ := json.Marshal(&replyMsg)
	log.Infof("Sending 200 OK to HTTP webhook. Request=%s Msg=%s", replyMsg.Request, replyMsg.Msg)
	res.WriteHeader(200)
	res.Write(reply)
	return
}

func (w *WebhooksBridge) webhookHandlerWithAck(res http.ResponseWriter, req *http.Request) {
	w.webhookHandler(res, req, true)
}

func (w *WebhooksBridge) webhookHandlerNoAck(res http.ResponseWriter, req *http.Request) {
	w.webhookHandler(res, req, false)
}

func (w *WebhooksBridge) webhookHandler(res http.ResponseWriter, req *http.Request, ack bool) {

	if req.ContentLength > MaxPayloadSize {
		errReply(res, fmt.Errorf("Message exceeds maximum allowable size"), 400)
		return
	}
	payloadToForward, err := ioutil.ReadAll(req.Body)
	if err != nil {
		errReply(res, fmt.Errorf("Unable to read input data: %s", err), 400)
		return
	}

	// We support both YAML and JSON input.
	// We parse the message into a generic string->interface map, that lets
	// us check a couple of routing fields needed to dispatch the messages
	// to Kafka (always in JSON). However, we do not perform full parsing.
	var genericPayload map[string]interface{}
	contentType := strings.ToLower(req.Header.Get("Content-type"))
	log.Infof("Received message 'Content-Type: %s' Length: %d", contentType, req.ContentLength)
	if contentType == "application/x-yaml" || contentType == "text/yaml" {
		yamlGenericPayload := make(map[interface{}]interface{})
		err := yaml.Unmarshal(payloadToForward, &yamlGenericPayload)
		if err != nil {
			errReply(res, fmt.Errorf("Unable to parse YAML: %s", err), 400)
			return
		}
		genericPayload = dyno.ConvertMapI2MapS(yamlGenericPayload).(map[string]interface{})
		// Reseialize back to JSON
		payloadToForward, err = json.Marshal(&genericPayload)
		if err != nil {
			errReply(res, fmt.Errorf("Unable to reserialize YAML payload as JSON: %s", err), 500)
			return
		}
	} else {
		genericPayload = make(map[string]interface{})
		err := json.Unmarshal(payloadToForward, &genericPayload)
		if err != nil {
			errReply(res, fmt.Errorf("Unable to parse JSON: %s", err), 400)
			return
		}
	}

	// Check we understand the type, and can get the key.
	// The rest of the validation is performed by the bridge listening to Kafka
	headers, exists := genericPayload["headers"]
	if !exists || reflect.TypeOf(headers).Kind() != reflect.Map {
		errReply(res, fmt.Errorf("Invalid message - missing 'headers' (or not an object)"), 400)
		return
	}
	msgType, exists := headers.(map[string]interface{})["type"]
	if !exists || reflect.TypeOf(msgType).Kind() != reflect.String {
		errReply(res, fmt.Errorf("Invalid message - missing 'headers.type' (or not a string)"), 400)
		return
	}
	var key string
	switch msgType {
	case kldmessages.MsgTypeDeployContract, kldmessages.MsgTypeSendTransaction:
		from, exists := genericPayload["from"]
		if !exists || reflect.TypeOf(from).Kind() != reflect.String {
			errReply(res, fmt.Errorf("Invalid message - missing 'from' (or not a string)"), 400)
			return
		}
		key = from.(string)
		break
	default:
		errReply(res, fmt.Errorf("Invalid message type: %s", msgType), 400)
		return
	}

	// We always generate the ID. It cannot be set by the user
	msgID := kldutils.UUIDv4()
	headers.(map[string]interface{})["id"] = msgID
	if ack {
		w.setMsgPending(msgID)
	}

	log.Debugf("Forwarding message to Kafka bridge: %s", payloadToForward)
	sentMsg := &sarama.ProducerMessage{
		Topic:    w.kafka.Conf().TopicOut,
		Key:      sarama.StringEncoder(key),
		Value:    sarama.ByteEncoder(payloadToForward),
		Metadata: msgID,
	}
	w.kafka.Producer().Input() <- sentMsg

	if ack {
		successMsg, err := w.waitForSend(msgID)
		if err != nil {
			errReply(res, fmt.Errorf("Failed to deliver message to Kafka: %s", err), 502)
			return
		}
		msgSentReply(res, ack, successMsg)
	} else {
		msgSentReply(res, ack, sentMsg)
	}
}

func (w *WebhooksBridge) statusHandler(res http.ResponseWriter, req *http.Request) {
	okReply(res)
}

// Start kicks off the HTTP and Kafka listeners
func (w *WebhooksBridge) Start() (err error) {

	mux := http.NewServeMux()
	mux.Handle("/hook", http.HandlerFunc(w.webhookHandlerWithAck))
	mux.Handle("/fasthook", http.HandlerFunc(w.webhookHandlerNoAck))
	mux.Handle("/status", http.HandlerFunc(w.statusHandler))

	tlsConfig, err := kldutils.CreateTLSConfiguration(&w.conf.HTTP.TLS)
	if err != nil {
		return
	}

	w.srv = &http.Server{
		Addr:           fmt.Sprintf("%s:%d", w.conf.HTTP.LocalAddr, w.conf.HTTP.Port),
		TLSConfig:      tlsConfig,
		Handler:        mux,
		MaxHeaderBytes: MaxHeaderSize,
	}

	// Wait until Kafka is up before we listen
	go func() {
		for w.kafka.Producer() == nil {
			time.Sleep(500)
		}
		log.Printf("Listening on %s", w.srv.Addr)
		if err = w.srv.ListenAndServe(); err != nil {
			return
		}
	}()

	// Defer to KafkaCommon processing
	err = w.kafka.Start()

	// Ensure we shutdown the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	w.srv.Shutdown(ctx)
	defer cancel()

	return
}
