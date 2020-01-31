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

package kldkafka

import (
	"context"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	log "github.com/sirupsen/logrus"
)

const (
	kafkaConsumerReconnectDelaySecs = 5
)

// KafkaGoRoutines defines goroutines for processing Kafka messages from KafkaCommon
type KafkaGoRoutines interface {
	ConsumerMessagesLoop(consumer KafkaConsumer, producer KafkaProducer, wg *sync.WaitGroup)
	ProducerErrorLoop(consumer KafkaConsumer, producer KafkaProducer, wg *sync.WaitGroup)
	ProducerSuccessLoop(consumer KafkaConsumer, producer KafkaProducer, wg *sync.WaitGroup)
}

// KafkaProducer provides the interface passed from KafkaCommon to produce messages (subset of sarama)
type KafkaProducer interface {
	AsyncClose()
	Input() chan<- *sarama.ProducerMessage
	Successes() <-chan *sarama.ProducerMessage
	Errors() <-chan *sarama.ProducerError
}

// KafkaConsumer provides the interface passed from KafkaCommon to consume messages
type KafkaConsumer interface {
	Close() error
	Messages() <-chan *sarama.ConsumerMessage
	Errors() <-chan error
	MarkOffset(*sarama.ConsumerMessage, string)
}

// KafkaFactory builds new clients
type KafkaFactory interface {
	NewClient(KafkaCommon, *sarama.Config) (KafkaClient, error)
}

// KafkaClient is the kafka client
type KafkaClient interface {
	NewProducer(KafkaCommon) (KafkaProducer, error)
	NewConsumer(KafkaCommon) (KafkaConsumer, error)
	Brokers() []*sarama.Broker
}

// SaramaKafkaFactory - uses sarama
type SaramaKafkaFactory struct{}

// NewClient - returns a new client
func (f *SaramaKafkaFactory) NewClient(k KafkaCommon, clientConf *sarama.Config) (c KafkaClient, err error) {
	var client sarama.Client
	if client, err = sarama.NewClient(k.Conf().Brokers, clientConf); err == nil {
		c = &saramaKafkaClient{client: client}
	}
	return
}

type saramaKafkaClient struct {
	client sarama.Client
}

func (c *saramaKafkaClient) Brokers() []*sarama.Broker {
	return c.client.Brokers()
}

func (c *saramaKafkaClient) NewProducer(k KafkaCommon) (KafkaProducer, error) {
	return sarama.NewAsyncProducerFromClient(c.client)
}

func (c *saramaKafkaClient) NewConsumer(k KafkaCommon) (KafkaConsumer, error) {
	h := newSaramaKafkaConsumerGroupHandler(
		&saramaConsumerGroupFactory{},
		c.client, k.Conf().ConsumerGroup,
		[]string{k.Conf().TopicIn},
		kafkaConsumerReconnectDelaySecs*time.Second)
	return h, nil
}

type consumerGroupFactory interface {
	NewConsumerGroupFromClient(groupID string, client sarama.Client) (sarama.ConsumerGroup, error)
}

type saramaConsumerGroupFactory struct{}

func (f *saramaConsumerGroupFactory) NewConsumerGroupFromClient(groupID string, client sarama.Client) (sarama.ConsumerGroup, error) {
	return sarama.NewConsumerGroupFromClient(groupID, client)
}

type saramaKafkaConsumerGroupHandler struct {
	group          string
	topics         []string
	closed         bool
	f              consumerGroupFactory
	c              sarama.Client
	cg             sarama.ConsumerGroup
	reconnectDelay time.Duration
	messages       chan *sarama.ConsumerMessage
	errors         chan error
	session        sarama.ConsumerGroupSession
	wg             sync.WaitGroup
}

func newSaramaKafkaConsumerGroupHandler(f consumerGroupFactory, c sarama.Client, group string, topics []string, reconnectDelay time.Duration) *saramaKafkaConsumerGroupHandler {
	h := &saramaKafkaConsumerGroupHandler{
		group:          group,
		topics:         topics,
		closed:         false,
		f:              f,
		c:              c,
		reconnectDelay: reconnectDelay,
		messages:       make(chan *sarama.ConsumerMessage),
		errors:         make(chan error),
	}
	h.wg.Add(1)
	go h.consumerGoRoutine()
	return h
}

func (h *saramaKafkaConsumerGroupHandler) consumerGoRoutine() {
	for !h.closed {
		log.Infof("Kafka consumer starting. Group: '%s' Topics: %+v", h.group, h.topics)

		var err error
		var wg sync.WaitGroup
		h.cg, err = h.f.NewConsumerGroupFromClient(h.group, h.c)
		if err != nil {
			log.Errorf("Failed to create consumer: %s", err)
		} else {
			// Go func to pass through errors
			wg.Add(1)
			go func() {
				for err := range h.cg.Errors() {
					h.errors <- err
				}
				wg.Done()
			}()

			err = h.cg.Consume(context.Background(), h.topics, h)
			log.Infof("Consumer completed: %s", err)
			if !h.closed {
				h.cg.Close()
			}
			wg.Wait()
		}
		h.cg = nil

		if !h.closed {
			time.Sleep(h.reconnectDelay)
		}
	}
	close(h.errors)
	close(h.messages)
	h.wg.Done()
}

func (h *saramaKafkaConsumerGroupHandler) Setup(session sarama.ConsumerGroupSession) error {
	h.session = session
	log.Infof("Consumer session setup. Claims=%+v Member=%s Generation=%d", session.Claims(), session.MemberID(), session.GenerationID())
	return nil
}

func (h *saramaKafkaConsumerGroupHandler) Cleanup(session sarama.ConsumerGroupSession) error {
	log.Infof("Consumer session cleanup. Claims=%+v Member=%s Generation=%d", session.Claims(), session.MemberID(), session.GenerationID())
	h.session = nil
	return nil
}

func (h *saramaKafkaConsumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		h.messages <- msg
	}
	return nil
}

func (h *saramaKafkaConsumerGroupHandler) Close() error {
	h.closed = true
	if h.cg != nil {
		return h.cg.Close()
	}
	return nil
}

func (h *saramaKafkaConsumerGroupHandler) Messages() <-chan *sarama.ConsumerMessage {
	return h.messages
}

func (h *saramaKafkaConsumerGroupHandler) Errors() <-chan error {
	return h.errors
}

func (h *saramaKafkaConsumerGroupHandler) MarkOffset(msg *sarama.ConsumerMessage, metadata string) {
	session := h.session
	if session != nil {
		session.MarkMessage(msg, metadata)
	}
}
