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
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	cluster "github.com/bsm/sarama-cluster"
	"github.com/stretchr/testify/assert"
)

type testKafkaFactory struct {
}

var testClientConf *cluster.Config

func (f testKafkaFactory) newClient(k *KafkaBridge, clientConf *cluster.Config) (c kafkaClient, err error) {
	testClientConf = clientConf
	c = testKafkaClient{}
	return
}

type testKafkaProducer struct {
	input     chan *sarama.ProducerMessage
	successes chan *sarama.ProducerMessage
	errors    chan *sarama.ProducerError
}

func (p testKafkaProducer) Close() error {
	if p.input != nil {
		close(p.input)
	}
	if p.successes != nil {
		close(p.successes)
	}
	if p.errors != nil {
		close(p.errors)
	}
	return nil
}

func (p testKafkaProducer) Input() chan<- *sarama.ProducerMessage {
	p.input = make(chan *sarama.ProducerMessage)
	return p.input
}

func (p testKafkaProducer) Successes() <-chan *sarama.ProducerMessage {
	p.successes = make(chan *sarama.ProducerMessage)
	return p.successes
}

func (p testKafkaProducer) Errors() <-chan *sarama.ProducerError {
	p.errors = make(chan *sarama.ProducerError)
	return p.errors
}

type testKafkaConsumer struct {
	messages      chan *sarama.ConsumerMessage
	notifications chan *cluster.Notification
	errors        chan error
}

func (c testKafkaConsumer) Close() error {
	if c.messages != nil {
		close(c.messages)
	}
	if c.notifications != nil {
		close(c.notifications)
	}
	if c.errors != nil {
		close(c.errors)
	}
	return nil
}

func (c testKafkaConsumer) Messages() <-chan *sarama.ConsumerMessage {
	c.messages = make(chan *sarama.ConsumerMessage)
	return c.messages
}

func (c testKafkaConsumer) Notifications() <-chan *cluster.Notification {
	c.notifications = make(chan *cluster.Notification)
	return c.notifications
}

func (c testKafkaConsumer) Errors() <-chan error {
	c.errors = make(chan error)
	return c.errors
}

type testKafkaClient struct{}

func (c testKafkaClient) Brokers() []*sarama.Broker {
	return []*sarama.Broker{}
}

func (c testKafkaClient) newProducer(k *KafkaBridge) (kafkaProducer, error) {
	return testKafkaProducer{}, nil
}

func (c testKafkaClient) newConsumer(k *KafkaBridge) (kafkaConsumer, error) {
	return testKafkaConsumer{}, nil
}

func execBridgeWithArgs(testArgs []string) (k *KafkaBridge, err error) {
	var f testKafkaFactory
	k = &KafkaBridge{}
	k.factory = f

	kafkaCmd := k.CobraInit()
	kafkaCmd.SetArgs(testArgs)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err = kafkaCmd.Execute()
		wg.Done()
	}()
	for k.signals == nil {
		time.Sleep(10 * time.Millisecond)
	}
	k.signals <- os.Interrupt
	wg.Wait()

	return
}

func TestExecuteWithIncompleteArgs(t *testing.T) {
	assert := assert.New(t)

	var k KafkaBridge
	var f testKafkaFactory
	k.factory = f

	kafkaCmd := k.CobraInit()

	testArgs := []string{}

	kafkaCmd.SetArgs(testArgs)
	err := kafkaCmd.Execute()
	assert.Equal(err.Error(), "No output topic specified for bridge to send events to")
	testArgs = append(testArgs, []string{"-T", "test"}...)

	kafkaCmd.SetArgs(testArgs)
	err = kafkaCmd.Execute()
	assert.Equal(err.Error(), "No input topic specified for bridge to listen to")
	testArgs = append(testArgs, []string{"-t", "test-in"}...)

	kafkaCmd.SetArgs(testArgs)
	err = kafkaCmd.Execute()
	assert.Equal(err.Error(), "No consumer group specified")
	testArgs = append(testArgs, []string{"-g", "test-group"}...)

	kafkaCmd.SetArgs(testArgs)
	err = kafkaCmd.Execute()
	assert.Equal(err.Error(), "No JSON/RPC URL set for ethereum node")
	testArgs = append(testArgs, []string{"-r", "https://someurl.example.com"}...)

	testArgs = append(testArgs, []string{"--tls-clientcerts", "/some/file"}...)
	kafkaCmd.SetArgs(testArgs)
	err = kafkaCmd.Execute()
	assert.Equal("flag mismatch: 'tls-clientcerts' set and 'tls-clientkey' unset", err.Error())
	testArgs = append(testArgs, []string{"--tls-clientkey", "somekey"}...)

	testArgs = append(testArgs, []string{"--sasl-username", "testuser"}...)
	kafkaCmd.SetArgs(testArgs)
	err = kafkaCmd.Execute()
	assert.Equal("flag mismatch: 'sasl-username' set and 'sasl-password' unset", err.Error())
	testArgs = append(testArgs, []string{"--sasl-password", "testpass"}...)
}

func TestExecuteWithNoTLS(t *testing.T) {
	assert := assert.New(t)

	_, err := execBridgeWithArgs([]string{
		"-r", "https://testrpc.example.com",
		"-t", "in-topic",
		"-T", "out-topic",
		"-g", "test-group",
	})

	assert.Equal(nil, err)
	assert.Regexp(true, testClientConf.Producer.Return.Successes)
	assert.Regexp(true, testClientConf.Producer.Return.Errors)
	assert.Regexp(sarama.WaitForLocal, testClientConf.Producer.RequiredAcks)
	assert.Regexp(500*time.Millisecond, testClientConf.Producer.Flush.Frequency)
	assert.Regexp(true, testClientConf.Consumer.Return.Errors)
	assert.Regexp(true, testClientConf.Group.Return.Notifications)
	assert.Regexp(false, testClientConf.Net.TLS.Enable)
	assert.Regexp(nil, testClientConf.Net.TLS.Config)
	assert.Regexp("\\w+", testClientConf.ClientID) // generated UUID

}
