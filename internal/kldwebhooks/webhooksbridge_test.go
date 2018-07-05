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
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/kaleido-io/ethconnect/internal/kldkafka"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

type testKafkaCommon struct {
	stop               chan bool
	startCalled        bool
	startErr           error
	cobraInitCalled    bool
	cobraPreRunECalled bool
	kafkaFactory       *kldkafka.MockKafkaFactory
	kafkaInitDelay     int
	startTime          time.Time
}

func (k *testKafkaCommon) Start() error {
	k.startCalled = true
	log.Infof("Test KafkaCommon started")
	<-k.stop
	log.Infof("Test KafkaCommon stopped")
	return k.startErr
}

func (k *testKafkaCommon) CobraInit(cmd *cobra.Command) {
	k.cobraInitCalled = true
}

func (k *testKafkaCommon) CobraPreRunE(cmd *cobra.Command) error {
	k.cobraPreRunECalled = true
	return nil
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

// newTestKafkaCommon creates a Webhooks instance with a Cobra command wrapper
func newTestWebhooks(testArgs []string, kafkaInitDelay int) (*WebhooksBridge, *testKafkaCommon) {
	log.SetLevel(log.DebugLevel)
	kafka := &testKafkaCommon{}
	kafka.startTime = time.Now()
	kafka.kafkaInitDelay = kafkaInitDelay
	kafka.stop = make(chan bool)
	kafka.kafkaFactory = kldkafka.NewMockKafkaFactory()
	w := NewWebhooksBridge()
	w.kafka = kafka
	cmd := w.CobraInit()
	cmd.SetArgs(testArgs)
	go func() {
		cmd.Execute()
	}()
	status := -1
	for status != 200 {
		statusURL := fmt.Sprintf("http://localhost:%d/status", w.conf.Port)
		resp, err := http.Get(statusURL)
		if err == nil {
			status = resp.StatusCode
		}
		log.Infof("Waiting for Webhook server to start (URL=%s Status=%d Err=%s)", statusURL, status, err)
		if status != 200 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return w, kafka
}

func TestStartStopDefaultArgs(t *testing.T) {
	assert := assert.New(t)

	w, kafka := newTestWebhooks([]string{}, 0)

	assert.Equal(8080, w.conf.Port)    // default
	assert.Equal("", w.conf.LocalAddr) // default

	kafka.stop <- true
}

func TestStartStopCustomArgs(t *testing.T) {
	assert := assert.New(t)

	w, kafka := newTestWebhooks([]string{"-l", "8081", "-L", "127.0.0.1"}, 0)

	assert.Equal(8081, w.conf.Port)
	assert.Equal("127.0.0.1", w.conf.LocalAddr)
	assert.Equal("127.0.0.1:8081", w.srv.Addr)

	kafka.stop <- true
}

func TestStartStopKafkaInitDelay(t *testing.T) {
	_, kafka := newTestWebhooks([]string{}, 500)

	kafka.stop <- true
}
