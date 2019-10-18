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
	"fmt"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/golang/mock/gomock"
	"github.com/kaleido-io/ethconnect/internal/kldkafka/mock_sarama"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type mockConsumerGroupFactory struct {
	mcg    *mock_sarama.MockConsumerGroup
	err    error
	called bool
}

func (m *mockConsumerGroupFactory) NewConsumerGroupFromClient(groupID string, client sarama.Client) (sarama.ConsumerGroup, error) {
	m.called = true
	return m.mcg, m.err
}

func TestConsumerGroupHandler(t *testing.T) {
	assert := assert.New(t)
	log.SetLevel(log.DebugLevel)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mc := mock_sarama.NewMockClient(ctrl)
	mcg := mock_sarama.NewMockConsumerGroup(ctrl)
	mf := &mockConsumerGroupFactory{
		mcg: mcg,
	}
	ms := mock_sarama.NewMockConsumerGroupSession(ctrl)
	mcgc := mock_sarama.NewMockConsumerGroupClaim(ctrl)

	stopConsume := make(chan bool)
	consumeOnce := make(chan bool)
	errors := make(chan error)
	messages := make(chan *sarama.ConsumerMessage)
	ms.EXPECT().Claims().MinTimes(1)
	ms.EXPECT().MemberID().MinTimes(1)
	ms.EXPECT().GenerationID().MinTimes(1)
	ms.EXPECT().MarkMessage(gomock.Any(), gomock.Any()).Times(1)
	mcgc.EXPECT().Messages().Return(messages).Times(1)
	mcg.EXPECT().Close().Return(nil).Times(1)
	mcg.EXPECT().Errors().Return(errors).Times(1)
	mcg.EXPECT().Consume(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, topics []string, handler sarama.ConsumerGroupHandler) error {
			assert.Equal([]string{"topic1"}, topics)
			t.Logf("Starting consume")
			handler.Setup(ms)
			t.Logf("Setup completed")
			handler.ConsumeClaim(ms, mcgc)
			consumeOnce <- true
			<-stopConsume
			handler.Cleanup(ms)
			t.Logf("Cleanup completed")
			return nil
		}).MinTimes(1)

	h := newSaramaKafkaConsumerGroupHandler(mf, mc, "group1", []string{"topic1"}, 10*time.Millisecond)
	go func() {
		msg := &sarama.ConsumerMessage{}
		messages <- msg
		errors <- fmt.Errorf("sample error")
		h.MarkOffset(msg, "mymeta")
		close(messages)
		<-consumeOnce
		stopConsume <- true
		h.Close()
	}()

	<-h.Messages()
	<-h.Errors()
	close(errors)
	h.wg.Wait()
	close(stopConsume)
	close(consumeOnce)
}

func TestConsumerGroupHandlerCreateFail(t *testing.T) {
	log.SetLevel(log.DebugLevel)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mc := mock_sarama.NewMockClient(ctrl)
	mf := &mockConsumerGroupFactory{
		err: fmt.Errorf("pop"),
	}

	h := newSaramaKafkaConsumerGroupHandler(mf, mc, "group1", []string{"topic1"}, 10*time.Millisecond)
	for !mf.called {
		time.Sleep(10 * time.Millisecond)
	}

	h.Close()
	h.wg.Wait()
}

func TestConsumerGroupHandlerReconnect(t *testing.T) {
	log.SetLevel(log.DebugLevel)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mc := mock_sarama.NewMockClient(ctrl)
	mcg := mock_sarama.NewMockConsumerGroup(ctrl)
	mf := &mockConsumerGroupFactory{
		mcg: mcg,
	}

	consumeOnce := make(chan bool)
	errors := make(chan error)
	firstLoop := true
	mcg.EXPECT().Close().Return(nil).MinTimes(1)
	mcg.EXPECT().Errors().Return(errors).MinTimes(1)
	mcg.EXPECT().Consume(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, topics []string, handler sarama.ConsumerGroupHandler) error {
			if firstLoop {
				consumeOnce <- true
				firstLoop = false
			}
			return fmt.Errorf("pop")
		}).MinTimes(1)

	h := newSaramaKafkaConsumerGroupHandler(mf, mc, "group1", []string{"topic1"}, 10*time.Millisecond)
	go func() {
		<-consumeOnce
		h.Close()
	}()

	close(errors)
	h.wg.Wait()
	close(consumeOnce)
}
