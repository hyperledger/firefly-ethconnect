// Copyright 2018, 2021 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kafka

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/hyperledger/firefly-ethconnect/mocks/saramamocks"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockConsumerGroupFactory struct {
	mcg    *saramamocks.ConsumerGroup
	err    error
	called bool
}

func (m *mockConsumerGroupFactory) NewConsumerGroupFromClient(groupID string, client sarama.Client) (sarama.ConsumerGroup, error) {
	m.called = true
	return m.mcg, m.err
}

func TestConsumerGroupHandler(t *testing.T) {
	log.SetLevel(log.DebugLevel)

	mc := &saramamocks.Client{}
	mcg := &saramamocks.ConsumerGroup{}
	mf := &mockConsumerGroupFactory{
		mcg: mcg,
	}
	ms := &saramamocks.ConsumerGroupSession{}
	mcgc := &saramamocks.ConsumerGroupClaim{}

	stopConsume := make(chan bool)
	consumeOnce := make(chan bool)
	errors := make(chan error)
	messages := make(chan *sarama.ConsumerMessage)

	InitCircuitBreaker(&CircuitBreakerConf{
		Enabled:    true,
		UpperBound: 1, // Trip immediately
	})
	cb := GetCircuitBreaker()

	ms.On("Claims").Return(nil)
	ms.On("MemberID").Return("")
	ms.On("GenerationID").Return(int32(0))
	ms.On("MarkMessage", mock.Anything, "mymeta").Return().Once()
	mcg.On("Errors").Return((<-chan error)(errors))
	mcg.On("Close").Return(nil).Once()
	mcgc.On("Messages").Return((<-chan *sarama.ConsumerMessage)(messages))
	mcgc.On("Topic").Return("topic1")
	mcgc.On("Partition").Return(int32(0))
	mcgc.On("HighWaterMarkOffset").Return(int64(1000))
	mcg.On("Consume", context.Background(), []string{"topic1"}, mock.Anything).
		Run(func(args mock.Arguments) {
			handler := args[2].(sarama.ConsumerGroupHandler)
			handler.Setup(ms)
			handler.ConsumeClaim(ms, mcgc)
			consumeOnce <- true
			<-stopConsume
			handler.Cleanup(ms)
		}).
		Return(nil)

	h := newSaramaKafkaConsumerGroupHandler(mf, mc, "group1", []string{"topic1"}, 10*time.Millisecond)
	go func() {
		msg := &sarama.ConsumerMessage{
			Value: []byte("hello world"),
		}
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

	mc.AssertExpectations(t)
	mcg.AssertExpectations(t)
	ms.AssertExpectations(t)
	mcgc.AssertExpectations(t)

	assert.Regexp(t, "too large", cb.Check("topic1"))
	singletonCircuitBreaker = nil
}

func TestConsumerGroupHandlerCreateFail(t *testing.T) {
	log.SetLevel(log.DebugLevel)

	mc := &saramamocks.Client{}
	mf := &mockConsumerGroupFactory{
		err: fmt.Errorf("pop"),
	}

	h := newSaramaKafkaConsumerGroupHandler(mf, mc, "group1", []string{"topic1"}, 10*time.Millisecond)
	for !mf.called {
		time.Sleep(10 * time.Millisecond)
	}

	h.Close()
	h.wg.Wait()

	mc.AssertExpectations(t)
}

func TestConsumerGroupHandlerReconnect(t *testing.T) {
	log.SetLevel(log.DebugLevel)

	mc := &saramamocks.Client{}
	mcg := &saramamocks.ConsumerGroup{}
	mf := &mockConsumerGroupFactory{
		mcg: mcg,
	}

	consumeOnce := make(chan bool)
	errors := make(chan error)
	firstLoop := true

	mcg.On("Close").Return(nil)
	mcg.On("Errors").Return((<-chan error)(errors))
	mconsume := mcg.On("Consume", context.Background(), []string{"topic1"}, mock.Anything)
	mconsume.RunFn = func(args mock.Arguments) {
		if firstLoop {
			consumeOnce <- true
			firstLoop = false
		}
		mconsume.ReturnArguments = mock.Arguments{fmt.Errorf("pop")}
	}

	h := newSaramaKafkaConsumerGroupHandler(mf, mc, "group1", []string{"topic1"}, 10*time.Millisecond)
	go func() {
		<-consumeOnce
		h.Close()
	}()

	close(errors)
	h.wg.Wait()
	close(consumeOnce)

	mc.AssertExpectations(t)
	mcg.AssertExpectations(t)
}
