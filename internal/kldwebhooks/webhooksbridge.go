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
	"net/http"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/kaleido-io/ethconnect/internal/kldkafka"
	"github.com/spf13/cobra"
)

// WebhooksBridge receives messages over HTTP POST and sends them to Kafka
type WebhooksBridge struct {
	Conf struct {
	}
	kafka kldkafka.KafkaCommon
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

func (w *WebhooksBridge) webhookHandler(res http.ResponseWriter, req *http.Request) {
}

// Start kicks off the bridge
func (w *WebhooksBridge) Start() (err error) {

	mux := http.NewServeMux()
	th := http.HandlerFunc(w.webhookHandler)
	mux.Handle("/message", th)

	tlsConfig, err := w.kafka.CreateTLSConfiguration()
	if err != nil {
		return
	}

	srv := &http.Server{
		Addr:      ":8080",
		TLSConfig: tlsConfig,
		Handler:   mux,
	}

	log.Printf("Listening on :3000")
	if err = srv.ListenAndServe(); err != nil {
		return
	}

	// Defer to KafkaCommon processing
	err = w.kafka.Start()
	return
}
