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
	log "github.com/sirupsen/logrus"

	"github.com/kaleido-io/ethconnect/internal/kldkafka"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
	"github.com/spf13/cobra"
)

const (
	// MaxHeaderSize max size of content
	MaxHeaderSize = 16 * 1024
	// MaxPayloadSize max size of content
	MaxPayloadSize = 128 * 1024
)

// WebhooksBridge receives messages over HTTP POST and sends them to Kafka
type WebhooksBridge struct {
	conf struct {
		LocalAddr string
		Port      int
	}
	kafka kldkafka.KafkaCommon
	srv   *http.Server
}

// NewWebhooksBridge constructor
func NewWebhooksBridge() (w *WebhooksBridge) {
	w = &WebhooksBridge{}
	kf := &kldkafka.SaramaKafkaFactory{}
	w.kafka = kldkafka.NewKafkaCommon(kf, w)
	return
}

// CobraInit retruns a cobra command to configure this KafkaBridge
func (w *WebhooksBridge) CobraInit() (cmd *cobra.Command) {
	cmd = &cobra.Command{
		Use:   "webhooks",
		Short: "Webhooks bridge to Kafka",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			err = w.Start()
			return
		},
		PreRunE: func(cmd *cobra.Command, args []string) (err error) {
			if err = w.kafka.CobraPreRunE(cmd); err != nil {
				return
			}
			return
		},
	}
	w.kafka.CobraInit(cmd)
	cmd.Flags().StringVarP(&w.conf.LocalAddr, "listen-addr", "L", os.Getenv("WEBHOOKS_LISTEN_ADDR"), "Local address to listen on")
	cmd.Flags().IntVarP(&w.conf.Port, "listen-port", "l", kldutils.DefInt("WEBHOOKS_LISTEN_PORT", 8080), "Port to listen on")
	return
}

// ConsumerMessagesLoop - consume replies
func (w *WebhooksBridge) ConsumerMessagesLoop(consumer kldkafka.KafkaConsumer, producer kldkafka.KafkaProducer, wg *sync.WaitGroup) {
	for msg := range consumer.Messages() {
		log.Infof("Webhooks received reply: %s", msg)
	}
	wg.Done()
}

// ProducerErrorLoop - consume errors
func (w *WebhooksBridge) ProducerErrorLoop(consumer kldkafka.KafkaConsumer, producer kldkafka.KafkaProducer, wg *sync.WaitGroup) {
	for err := range producer.Errors() {
		log.Errorf("Webhooks received error: %s", err)
	}
	wg.Done()
}

// ProducerSuccessLoop - consume successes
func (w *WebhooksBridge) ProducerSuccessLoop(consumer kldkafka.KafkaConsumer, producer kldkafka.KafkaProducer, wg *sync.WaitGroup) {
	for msg := range producer.Successes() {
		log.Infof("Webhooks sent message ok: %s", msg)
	}
	wg.Done()
}

type httpError struct {
	Message string `json:"error"`
}

func errReply(res http.ResponseWriter, err error, status int) {
	reply, _ := json.Marshal(&httpError{Message: err.Error()})
	res.Write(reply)
	res.WriteHeader(400)
	return
}

func (w *WebhooksBridge) webhookHandler(res http.ResponseWriter, req *http.Request) {

	if req.ContentLength > MaxPayloadSize {
		errReply(res, fmt.Errorf("Message exceeds maximum allowable size"), 400)
		return
	}
	originalPayload, err := ioutil.ReadAll(req.Body)
	if err != nil {
		errReply(res, fmt.Errorf("Unable to read input dataL: %s", err), 400)
		return
	}

	genericPayload := make(map[string]interface{})
	contentType := strings.ToLower(req.Header.Get("Content-type"))
	log.Infof("Received message 'Content-Type: %s' Length: %d", contentType, req.ContentLength)
	if contentType == "application/x-yaml" || contentType == "text/yaml" {
		err := yaml.Unmarshal(originalPayload, genericPayload)
		if err != nil {
			errReply(res, fmt.Errorf("Unable to parse YAML: %s", err), 400)
			return
		}
	} else {
		err := json.Unmarshal(originalPayload, genericPayload)
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
	msgType, exists := headers.(map[interface{}]interface{})["type"]
	if !exists || reflect.TypeOf(msgType).Kind() != reflect.String {
		errReply(res, fmt.Errorf("Invalid message - missing 'headers.type' (or not a string)"), 400)
		return
	}
	var key string
	switch msgType {
	case kldmessages.MsgTypeDeployContract:
	case kldmessages.MsgTypeSendTransaction:
		to, exists := genericPayload["to"]
		if !exists || reflect.TypeOf(to).Kind() != reflect.String {
			errReply(res, fmt.Errorf("Invalid message - missing 'to' (or not a string)"), 400)
			return
		}
		key = to.(string)
		break
	default:
		errReply(res, fmt.Errorf("Invalid message type: %s", msgType), 400)
		return
	}

	log.Debugf("Forwarding message to Kafka bridge: %s", originalPayload)
	w.kafka.Producer().Input() <- &sarama.ProducerMessage{
		Topic: w.kafka.Conf().TopicOut,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(originalPayload),
	}

	res.WriteHeader(204)
}

type statusMsg struct {
	OK bool `json:"ok"`
}

func (w *WebhooksBridge) statusHandler(res http.ResponseWriter, req *http.Request) {
	replyMsg := statusMsg{}
	replyMsg.OK = true
	replyBytes, err := json.Marshal(&replyMsg)
	if err != nil {
		errReply(res, fmt.Errorf("Failed to serialize status reply: %s", err), 500)
		return
	}
	res.Write(replyBytes)
	res.WriteHeader(200)
}

// Start kicks off the HTTP and Kafka listeners
func (w *WebhooksBridge) Start() (err error) {

	mux := http.NewServeMux()
	mux.Handle("/message", http.HandlerFunc(w.webhookHandler))
	mux.Handle("/status", http.HandlerFunc(w.statusHandler))

	tlsConfig, err := w.kafka.CreateTLSConfiguration()
	if err != nil {
		return
	}

	w.srv = &http.Server{
		Addr:           fmt.Sprintf("%s:%d", w.conf.LocalAddr, w.conf.Port),
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
