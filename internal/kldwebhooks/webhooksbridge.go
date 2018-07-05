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

type errMsg struct {
	Message string `json:"error"`
}

func errReply(res http.ResponseWriter, err error, status int) {
	reply, _ := json.Marshal(&errMsg{Message: err.Error()})
	res.WriteHeader(400)
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

func (w *WebhooksBridge) webhookHandler(res http.ResponseWriter, req *http.Request) {

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

	log.Debugf("Forwarding message to Kafka bridge: %s", payloadToForward)
	w.kafka.Producer().Input() <- &sarama.ProducerMessage{
		Topic: w.kafka.Conf().TopicOut,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(payloadToForward),
	}

	okReply(res)
}

func (w *WebhooksBridge) statusHandler(res http.ResponseWriter, req *http.Request) {
	okReply(res)
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
