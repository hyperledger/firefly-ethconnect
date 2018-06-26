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
	"github.com/Shopify/sarama"
	cluster "github.com/bsm/sarama-cluster"
)

// We define minimal interfaces for the parts of the sarama/sarama-cluster
// package we use, to allow for stubbing in our unit tests

type kafkaFactory interface {
	newClient(*KafkaBridge, *cluster.Config) (kafkaClient, error)
}

type kafkaClient interface {
	newProducer(*KafkaBridge) (kafkaProducer, error)
	newConsumer(*KafkaBridge) (kafkaConsumer, error)
	Brokers() []*sarama.Broker
}

type kafkaProducer interface {
	Close() error
	Input() chan<- *sarama.ProducerMessage
	Successes() <-chan *sarama.ProducerMessage
	Errors() <-chan *sarama.ProducerError
}

type kafkaConsumer interface {
	Close() error
	Messages() <-chan *sarama.ConsumerMessage
	Notifications() <-chan *cluster.Notification
	Errors() <-chan error
	MarkOffset(*sarama.ConsumerMessage, string)
}

type saramaKafkaFactory struct{}

func (f saramaKafkaFactory) newClient(k *KafkaBridge, clientConf *cluster.Config) (c kafkaClient, err error) {
	if client, err := cluster.NewClient(k.Conf.Brokers, clientConf); err == nil {
		c = saramaKafkaClient{client: client}
	}
	return
}

type saramaKafkaClient struct {
	client *cluster.Client
}

func (c saramaKafkaClient) Brokers() []*sarama.Broker {
	return c.client.Brokers()
}

func (c saramaKafkaClient) newProducer(k *KafkaBridge) (kafkaProducer, error) {
	return sarama.NewAsyncProducerFromClient(c.client.Client)
}

func (c saramaKafkaClient) newConsumer(k *KafkaBridge) (kafkaConsumer, error) {
	return cluster.NewConsumerFromClient(c.client, k.Conf.ConsumerGroup, []string{k.Conf.TopicIn})
}
