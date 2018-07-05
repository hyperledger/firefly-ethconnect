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
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/kaleido-io/ethconnect/internal/kldkafka"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

type testKafkaCommon struct {
	stop             chan bool
	startCalled      bool
	startErr         error
	cobraInitCalled  bool
	cobraPreRunError error
	kafkaFactory     *kldkafka.MockKafkaFactory
	kafkaInitDelay   int
	startTime        time.Time
}

func (k *testKafkaCommon) Start() error {
	k.startCalled = true
	log.Infof("Test KafkaCommon started")
	<-k.stop
	log.Infof("Test KafkaCommon stopped")
	if k.kafkaFactory.Consumer != nil {
		k.kafkaFactory.Consumer.Close()
	}
	if k.kafkaFactory.Producer != nil {
		k.kafkaFactory.Producer.AsyncClose()
	}
	return k.startErr
}

func (k *testKafkaCommon) CobraInit(cmd *cobra.Command) {
	k.cobraInitCalled = true
}

func (k *testKafkaCommon) CobraPreRunE(cmd *cobra.Command) error {
	return k.cobraPreRunError
}

func (k *testKafkaCommon) CreateTLSConfiguration() (t *tls.Config, err error) {
	return nil, nil
}

func (k *testKafkaCommon) Conf() *kldkafka.KafkaCommonConf {
	return &kldkafka.KafkaCommonConf{}
}

func (k *testKafkaCommon) Producer() kldkafka.KafkaProducer {
	var producer kldkafka.KafkaProducer
	timeSinceStart := time.Now().Sub(k.startTime)
	if timeSinceStart > time.Duration(k.kafkaInitDelay)*time.Millisecond {
		producer, _ = k.kafkaFactory.NewProducer(k)
	}
	return producer
}

var webhookExecuteError atomic.Value

func newTestKafkaComon() *testKafkaCommon {
	kafka := &testKafkaCommon{}
	kafka.startTime = time.Now()
	kafka.stop = make(chan bool)
	kafka.kafkaFactory = kldkafka.NewMockKafkaFactory()
	return kafka
}

// startTestWebhooks creates a Webhooks instance with a Cobra command wrapper, and executes it
// It returns once it's reached kafka initialization successfully, or errored during initialization
func startTestWebhooks(testArgs []string, kafka *testKafkaCommon) (*WebhooksBridge, error) {
	log.SetLevel(log.DebugLevel)
	w := NewWebhooksBridge()
	w.kafka = kafka
	cmd := w.CobraInit()
	cmd.SetArgs(testArgs)
	webhookExecuteError.Store(errors.New("none"))
	go func() {
		err := cmd.Execute()
		log.Infof("Kafka webhooks completed. Err=%s", err)
		if err != nil {
			webhookExecuteError.Store(err)
		}
	}()
	status := -1
	var err error
	for err == nil && status != 200 {
		statusURL := fmt.Sprintf("http://localhost:%d/status", w.conf.Port)
		resp, httpErr := http.Get(statusURL)
		if httpErr == nil {
			status = resp.StatusCode
		}
		errI := webhookExecuteError.Load()
		if errI != nil {
			err = errI.(error)
		}
		log.Infof("Waiting for Webhook server to start (URL=%s Status=%d HTTPErr=%s Err=%s)", statusURL, status, httpErr, err)
		if status != 200 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return w, err
}

func TestStartStopDefaultArgs(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	w, err := startTestWebhooks([]string{}, k)
	assert.Errorf(err, "none")

	assert.Equal(8080, w.conf.Port)    // default
	assert.Equal("", w.conf.LocalAddr) // default

	k.stop <- true
}

func TestStartStopCustomArgs(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	w, err := startTestWebhooks([]string{"-l", "8081", "-L", "127.0.0.1"}, k)
	assert.Errorf(err, "none")

	assert.Equal(8081, w.conf.Port)
	assert.Equal("127.0.0.1", w.conf.LocalAddr)
	assert.Equal("127.0.0.1:8081", w.srv.Addr)

	k.stop <- true
}

func TestStartStopKafkaInitDelay(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	k.kafkaInitDelay = 500
	_, err := startTestWebhooks([]string{}, k)
	assert.Errorf(err, "none")

	k.stop <- true
}

func TestStartStopKafkaPreRunError(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	k.cobraPreRunError = fmt.Errorf("pop")
	_, err := startTestWebhooks([]string{}, k)
	assert.Errorf(err, "pop")
}

func TestConsumerMessagesLoopIsNoop(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	w, err := startTestWebhooks([]string{}, k)
	assert.Errorf(err, "none")

	wg := &sync.WaitGroup{}
	wg.Add(1)
	consumer, _ := k.kafkaFactory.NewConsumer(k)
	producer, _ := k.kafkaFactory.NewProducer(k)

	go func() {
		w.ConsumerMessagesLoop(consumer, producer, wg)
	}()

	consumer.(*kldkafka.MockKafkaConsumer).MockMessages <- &sarama.ConsumerMessage{
		Value: []byte("hello world"),
	}

	k.stop <- true
	wg.Wait()

}

func TestProducerErrorLoopIsNoop(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	w, err := startTestWebhooks([]string{}, k)
	assert.Errorf(err, "none")

	wg := &sync.WaitGroup{}
	wg.Add(1)
	consumer, _ := k.kafkaFactory.NewConsumer(k)
	producer, _ := k.kafkaFactory.NewProducer(k)

	go func() {
		w.ProducerErrorLoop(consumer, producer, wg)
	}()

	producer.(*kldkafka.MockKafkaProducer).MockErrors <- &sarama.ProducerError{
		Err: fmt.Errorf("fizzle"),
	}

	k.stop <- true
	wg.Wait()

}

func TestProducerSuccessesLoopIsNoop(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	w, err := startTestWebhooks([]string{}, k)
	assert.Errorf(err, "none")

	wg := &sync.WaitGroup{}
	wg.Add(1)
	consumer, _ := k.kafkaFactory.NewConsumer(k)
	producer, _ := k.kafkaFactory.NewProducer(k)

	go func() {
		w.ProducerSuccessLoop(consumer, producer, wg)
	}()

	producer.(*kldkafka.MockKafkaProducer).MockSuccesses <- &sarama.ProducerMessage{}

	k.stop <- true
	wg.Wait()

}
