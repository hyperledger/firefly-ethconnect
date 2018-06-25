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
