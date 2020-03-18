// Copyright 2018, 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldrest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Shopify/sarama"
	"github.com/kaleido-io/ethconnect/internal/kldauth"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldkafka"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	log "github.com/sirupsen/logrus"
)

// webhooksKafka provides the HTTP -> Kafka bridge functionality for ethconnect
type webhooksKafka struct {
	kafka       kldkafka.KafkaCommon
	receipts    *receiptStore
	sendCond    *sync.Cond
	pendingMsgs map[string]bool
	successMsgs map[string]*sarama.ProducerMessage
	failedMsgs  map[string]error
	finished    bool
}

func newWebhooksKafkaBase(receipts *receiptStore) *webhooksKafka {
	return &webhooksKafka{
		receipts:    receipts,
		sendCond:    sync.NewCond(&sync.Mutex{}),
		pendingMsgs: make(map[string]bool),
		successMsgs: make(map[string]*sarama.ProducerMessage),
		failedMsgs:  make(map[string]error),
	}
}

// newWebhooksKafka constructor
func newWebhooksKafka(kconf *kldkafka.KafkaCommonConf, receipts *receiptStore) (w *webhooksKafka) {
	w = newWebhooksKafkaBase(receipts)
	kf := &kldkafka.SaramaKafkaFactory{}
	w.kafka = kldkafka.NewKafkaCommon(kf, kconf, w)
	return
}

func (w *webhooksKafka) setMsgPending(msgID string) {
	w.sendCond.L.Lock()
	w.pendingMsgs[msgID] = true
	w.sendCond.L.Unlock()
}

func (w *webhooksKafka) waitForSend(msgID string) (msg *sarama.ProducerMessage, err error) {
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

// ConsumerMessagesLoop - consume replies
func (w *webhooksKafka) ConsumerMessagesLoop(consumer kldkafka.KafkaConsumer, producer kldkafka.KafkaProducer, wg *sync.WaitGroup) {
	for msg := range consumer.Messages() {
		w.receipts.processReply(msg.Value)

		// Regardless of outcome, we ack
		consumer.MarkOffset(msg, "")
	}
	wg.Done()
}

// ProducerErrorLoop - consume errors
func (w *webhooksKafka) ProducerErrorLoop(consumer kldkafka.KafkaConsumer, producer kldkafka.KafkaProducer, wg *sync.WaitGroup) {
	log.Debugf("Webhooks listening for errors sending to Kafka")
	for err := range producer.Errors() {
		log.Errorf("Error sending message: %s", err)
		if err.Msg == nil || err.Msg.Metadata == nil {
			// This should not be possible
			panic(klderrors.Errorf(klderrors.WebhooksKafkaUnexpectedErrFmt, err))
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
func (w *webhooksKafka) ProducerSuccessLoop(consumer kldkafka.KafkaConsumer, producer kldkafka.KafkaProducer, wg *sync.WaitGroup) {
	log.Debugf("Webhooks listening for successful sends to Kafka")
	for msg := range producer.Successes() {
		log.Infof("Webhooks sent message ok: %s", msg.Metadata)
		if msg.Metadata == nil {
			// This should not be possible
			panic(klderrors.Errorf(klderrors.WebhooksKafkaDeliveryReportNoMeta, msg))
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

func (w *webhooksKafka) sendWebhookMsg(ctx context.Context, key, msgID string, msg map[string]interface{}, ack bool) (string, int, error) {

	// Reseialize back to JSON with the headers
	payloadToForward, err := json.Marshal(&msg)
	if err != nil {
		return "", 500, klderrors.Errorf(klderrors.WebhooksKafkaYAMLtoJSON, err)
	}
	if ack {
		w.setMsgPending(msgID)
	}

	log.Debugf("Message payload: %s", payloadToForward)
	sentMsg := &sarama.ProducerMessage{
		Topic:    w.kafka.Conf().TopicOut,
		Key:      sarama.StringEncoder(key),
		Value:    sarama.ByteEncoder(payloadToForward),
		Metadata: msgID,
	}
	accessToken := kldauth.GetAccessToken(ctx)
	if accessToken != "" {
		sentMsg.Headers = []sarama.RecordHeader{
			sarama.RecordHeader{
				Key:   []byte(kldmessages.RecordHeaderAccessToken),
				Value: []byte(accessToken),
			},
		}
	}
	w.kafka.Producer().Input() <- sentMsg

	msgAck := ""
	if ack {
		successMsg, err := w.waitForSend(msgID)
		if err != nil {
			return "", 502, klderrors.Errorf(klderrors.WebhooksKafkaErr, err)
		}
		msgAck = fmt.Sprintf("%s:%d:%d", successMsg.Topic, successMsg.Partition, successMsg.Offset)
	}
	return msgAck, 200, nil
}

func (w *webhooksKafka) validateConf() error {
	return w.kafka.ValidateConf()
}

func (w *webhooksKafka) run() error {
	err := w.kafka.Start()
	w.finished = true
	return err
}

func (w *webhooksKafka) isInitialized() bool {
	// We mark ourselves as ready once the kafka bridge has constructed its
	// producer, so it can accept messages.
	return (w.finished || w.kafka.Producer() != nil)
}
