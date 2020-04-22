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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/kaleido-io/ethconnect/internal/kldauth"
	"github.com/kaleido-io/ethconnect/internal/kldauth/kldauthtest"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	"github.com/kaleido-io/ethconnect/internal/kldtx"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

var kbMinWorkingArgs = []string{
	"-r", "https://testrpc.example.com",
}

type testKafkaCommon struct {
	startCalled     bool
	startErr        error
	validateErr     error
	cobraInitCalled bool
}

func (k *testKafkaCommon) Start() error {
	k.startCalled = true
	return k.startErr
}

func (k *testKafkaCommon) CobraInit(cmd *cobra.Command) {
	k.cobraInitCalled = true
}

func (k *testKafkaCommon) ValidateConf() error {
	return k.validateErr
}

func (k *testKafkaCommon) CreateTLSConfiguration() (t *tls.Config, err error) {
	return nil, nil
}

func (k *testKafkaCommon) Conf() *KafkaCommonConf {
	return &KafkaCommonConf{}
}

func (k *testKafkaCommon) SetConf(*KafkaCommonConf) {
	return
}

func (k *testKafkaCommon) Producer() KafkaProducer {
	return nil
}

type testKafkaMsgProcessor struct {
	messages chan kldtx.TxnContext
	rpc      kldeth.RPCClient
}

func (p *testKafkaMsgProcessor) Init(rpc kldeth.RPCClient) {
	p.rpc = rpc
}

func (p *testKafkaMsgProcessor) OnMessage(msg kldtx.TxnContext) {
	log.Infof("Dispatched message context to processor: %s", msg)
	p.messages <- msg
	return
}
func TestNewKafkaBridge(t *testing.T) {
	assert := assert.New(t)

	var printYAML = false
	bridge := NewKafkaBridge(&printYAML)
	var conf KafkaBridgeConf
	conf.RPC.URL = "http://example.com"
	bridge.SetConf(&conf)
	assert.Equal("http://example.com", bridge.Conf().RPC.URL)

	assert.NotNil(bridge.inFlight)
	assert.NotNil(bridge.inFlightCond)
}

func newTestKafkaBridge() (k *KafkaBridge, kafkaCmd *cobra.Command) {
	log.SetLevel(log.DebugLevel)
	var printYAML = false
	k = NewKafkaBridge(&printYAML)
	k.kafka = &testKafkaCommon{}
	k.processor = &testKafkaMsgProcessor{
		messages: make(chan kldtx.TxnContext),
	}
	kafkaCmd = k.CobraInit()
	return k, kafkaCmd
}
func TestExecuteBridgeWithIncompleteArgs(t *testing.T) {
	assert := assert.New(t)

	k, kafkaCmd := newTestKafkaBridge()
	testArgs := []string{}

	kafkaCmd.SetArgs(testArgs)
	err := kafkaCmd.Execute()
	assert.Equal(err.Error(), "No JSON/RPC URL set for ethereum node")
	testArgs = append(testArgs, []string{"-r", "http://localhost:8545"}...)

	testArgs = append(testArgs, []string{"--tx-timeout", "1"}...)
	kafkaCmd.SetArgs(testArgs)
	err = kafkaCmd.Execute()
	// Bumped up to minimum
	assert.Equal(10, k.conf.MaxTXWaitTime)
}

func TestExecuteBridgeWithIncompleteKafkaArgs(t *testing.T) {
	assert := assert.New(t)

	k, kafkaCmd := newTestKafkaBridge()
	k.kafka.(*testKafkaCommon).validateErr = fmt.Errorf("pop")
	testArgs := []string{}

	kafkaCmd.SetArgs(testArgs)
	err := kafkaCmd.Execute()
	assert.Equal(err.Error(), "pop")
}

func TestDefIntWithBadEnvVar(t *testing.T) {
	assert := assert.New(t)

	os.Setenv("KAFKA_MAX_INFLIGHT", "badness")
	defer os.Unsetenv("KAFKA_MAX_INFLIGHT")

	k, kafkaCmd := newTestKafkaBridge()
	kafkaCmd.SetArgs(kbMinWorkingArgs)
	err := kafkaCmd.Execute()

	assert.Nil(err)
	assert.Equal(10, k.conf.MaxInFlight)
}

func TestDefIntWithGoodEnvVar(t *testing.T) {
	assert := assert.New(t)

	os.Setenv("KAFKA_MAX_INFLIGHT", "123")
	defer os.Unsetenv("KAFKA_MAX_INFLIGHT")

	k, kafkaCmd := newTestKafkaBridge()
	kafkaCmd.SetArgs(kbMinWorkingArgs)
	err := kafkaCmd.Execute()

	assert.Nil(err)
	assert.Equal(123, k.conf.MaxInFlight)
}

func TestPrintYAML(t *testing.T) {
	assert := assert.New(t)

	k, kafkaCmd := newTestKafkaBridge()

	*k.printYAML = true
	kafkaCmd.SetArgs(kbMinWorkingArgs)
	err := kafkaCmd.Execute()

	assert.Nil(err)
}

func TestExecuteWithBadRPCURL(t *testing.T) {
	assert := assert.New(t)

	args := []string{"-r", "!!!bad!!!"}
	_, kafkaCmd := newTestKafkaBridge()
	kafkaCmd.SetArgs(args)
	err := kafkaCmd.Execute()

	assert.Regexp("connect", err.Error())

}

func setupMocks() (*KafkaBridge, *testKafkaMsgProcessor, *MockKafkaConsumer, *MockKafkaProducer, *sync.WaitGroup) {
	k, _ := newTestKafkaBridge()
	k.conf.MaxInFlight = 10
	f := NewMockKafkaFactory()
	mockConsumer, _ := f.NewConsumer(k.kafka)
	mockProducer, _ := f.NewProducer(k.kafka)
	wg := &sync.WaitGroup{}
	wg.Add(2)
	go k.ConsumerMessagesLoop(mockConsumer, mockProducer, wg)
	go k.ProducerSuccessLoop(mockConsumer, mockProducer, wg)
	processor := k.processor.(*testKafkaMsgProcessor)
	return k, processor, mockConsumer.(*MockKafkaConsumer), mockProducer.(*MockKafkaProducer), wg
}

func TestSingleMessageWithReply(t *testing.T) {
	assert := assert.New(t)
	kldauth.RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	_, processor, mockConsumer, mockProducer, wg := setupMocks()

	// Send a minimal test message
	msg1 := kldmessages.RequestCommon{}
	msg1.Headers.MsgType = "TestSingleMessageWithReply"
	msg1Ctx := map[string]interface{}{
		"some": "data",
	}
	msg1.Headers.Context = msg1Ctx
	msg1.Headers.Account = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg1bytes, err := json.Marshal(&msg1)
	log.Infof("Sent message: %s", string(msg1bytes))

	mockConsumer.MockMessages <- &sarama.ConsumerMessage{
		Topic:     "in-topic",
		Partition: 5,
		Offset:    500,
		Value:     msg1bytes,
		Headers: []*sarama.RecordHeader{
			&sarama.RecordHeader{
				Key:   []byte(kldmessages.RecordHeaderAccessToken),
				Value: []byte("testat"),
			},
		},
	}

	// Get the message via the processor
	msgContext1 := <-processor.messages
	assert.Equal("testat", kldauth.GetAccessToken(msgContext1.Context()))
	assert.Equal("verified", kldauth.GetAuthContext(msgContext1.Context()))
	assert.NotEmpty(msgContext1.Headers().ID) // Generated one as not supplied
	assert.Equal(msg1.Headers.MsgType, msgContext1.Headers().MsgType)
	assert.Equal("data", msgContext1.Headers().Context["some"])
	assert.Equal(len(msgContext1.(*msgContext).replyBytes), msgContext1.(*msgContext).Length())
	var msgUnmarshaled kldmessages.RequestCommon
	msgContext1.Unmarshal(&msgUnmarshaled)
	assert.Equal(msg1.Headers.MsgType, msgUnmarshaled.Headers.MsgType)

	// Send the reply in a go routine
	go func() {
		reply1 := kldmessages.ReplyCommon{}
		reply1.Headers.MsgType = "TestReply"
		msgContext1.Reply(&reply1)
	}()

	// Check the reply is sent correctly to Kafka
	replyKafkaMsg := <-mockProducer.MockInput
	mockProducer.MockSuccesses <- replyKafkaMsg
	replyBytes, err := replyKafkaMsg.Value.Encode()
	if err != nil {
		assert.Fail("Could not get bytes from reply: %s", err)
		return
	}
	var replySent kldmessages.ReplyCommon
	err = json.Unmarshal(replyBytes, &replySent)
	if err != nil {
		assert.Fail("Could not unmarshal reply: %s", err)
		return
	}
	assert.NotEmpty(replySent.Headers.ID)
	assert.NotEqual(msgContext1.Headers().ID, replySent.Headers.ID)
	assert.Equal(msgContext1.Headers().ID, replySent.Headers.ReqID)
	assert.Equal("in-topic:5:500", replySent.Headers.ReqOffset)
	assert.Equal("data", replySent.Headers.Context["some"])

	// Shut down
	mockProducer.AsyncClose()
	mockConsumer.Close()
	wg.Wait()

	kldauth.RegisterSecurityModule(nil)
}

func TestSingleMessageWithNotAuthorizedReply(t *testing.T) {
	assert := assert.New(t)
	kldauth.RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	_, _, mockConsumer, mockProducer, wg := setupMocks()

	// Send a minimal test message
	msg1 := kldmessages.RequestCommon{}
	msg1.Headers.MsgType = "TestSingleMessageWithErrorReply"
	msg1.Headers.Account = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg1bytes, err := json.Marshal(&msg1)
	log.Infof("Sent message: %s", string(msg1bytes))
	mockConsumer.MockMessages <- &sarama.ConsumerMessage{Value: msg1bytes}

	// Check the reply is sent correctly to Kafka
	replyKafkaMsg := <-mockProducer.MockInput
	mockProducer.MockSuccesses <- replyKafkaMsg
	replyBytes, err := replyKafkaMsg.Value.Encode()
	if err != nil {
		assert.Fail("Could not get bytes from reply: %s", err)
		return
	}
	var errorReply kldmessages.ErrorReply
	err = json.Unmarshal(replyBytes, &errorReply)
	if err != nil {
		assert.Fail("Could not unmarshal reply: %s", err)
		return
	}
	assert.Equal("Unauthorized", errorReply.ErrorMessage)

	// Shut down
	mockProducer.AsyncClose()
	mockConsumer.Close()
	wg.Wait()

	kldauth.RegisterSecurityModule(nil)
}

func TestSingleMessageWithErrorReply(t *testing.T) {
	assert := assert.New(t)

	_, processor, mockConsumer, mockProducer, wg := setupMocks()

	// Send a minimal test message
	msg1 := kldmessages.RequestCommon{}
	msg1.Headers.MsgType = "TestSingleMessageWithErrorReply"
	msg1.Headers.Account = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg1bytes, err := json.Marshal(&msg1)
	log.Infof("Sent message: %s", string(msg1bytes))
	mockConsumer.MockMessages <- &sarama.ConsumerMessage{Value: msg1bytes}

	// Get the message via the processor
	msgContext1 := <-processor.messages
	assert.NotEmpty(msgContext1.Headers().ID) // Generated one as not supplied
	go func() {
		msgContext1.SendErrorReply(400, fmt.Errorf("bang"))
	}()

	// Check the reply is sent correctly to Kafka
	replyKafkaMsg := <-mockProducer.MockInput
	mockProducer.MockSuccesses <- replyKafkaMsg
	replyBytes, err := replyKafkaMsg.Value.Encode()
	if err != nil {
		assert.Fail("Could not get bytes from reply: %s", err)
		return
	}
	var errorReply kldmessages.ErrorReply
	err = json.Unmarshal(replyBytes, &errorReply)
	if err != nil {
		assert.Fail("Could not unmarshal reply: %s", err)
		return
	}
	assert.Equal("bang", errorReply.ErrorMessage)

	// Shut down
	mockProducer.AsyncClose()
	mockConsumer.Close()
	wg.Wait()
}

func TestSingleMessageWithErrorReplyWithGapFillDetail(t *testing.T) {
	assert := assert.New(t)

	_, processor, mockConsumer, mockProducer, wg := setupMocks()

	// Send a minimal test message
	msg1 := kldmessages.RequestCommon{}
	msg1.Headers.MsgType = "TestSingleMessageWithErrorReply"
	msg1.Headers.Account = "0xAA983AD2a0e0eD8ac639277F37be42F2A5d2618c"
	msg1bytes, _ := json.Marshal(&msg1)
	log.Infof("Sent message: %s", string(msg1bytes))
	mockConsumer.MockMessages <- &sarama.ConsumerMessage{Value: msg1bytes}

	// Get the message via the processor
	msgContext1 := <-processor.messages
	assert.NotEmpty(msgContext1.Headers().ID) // Generated one as not supplied
	go func() {
		msgContext1.SendErrorReplyWithGapFill(400, fmt.Errorf("bang"), "txhash", true)
	}()

	// Check the reply is sent correctly to Kafka
	replyKafkaMsg := <-mockProducer.MockInput
	mockProducer.MockSuccesses <- replyKafkaMsg
	replyBytes, _ := replyKafkaMsg.Value.Encode()
	var errorReply kldmessages.ErrorReply
	json.Unmarshal(replyBytes, &errorReply)
	assert.Equal("bang", errorReply.ErrorMessage)
	assert.Equal("txhash", errorReply.GapFillTxHash)
	assert.Equal(true, *errorReply.GapFillSucceeded)

	// Shut down
	mockProducer.AsyncClose()
	mockConsumer.Close()
	wg.Wait()
}
func TestMoreMessagesThanMaxInFlight(t *testing.T) {
	assert := assert.New(t)

	k, processor, mockConsumer, mockProducer, wg := setupMocks()
	assert.Equal(10, k.conf.MaxInFlight)

	// Send 20 messages (10 is the max inflight)
	go func() {
		for i := 0; i < 20; i++ {
			msg := kldmessages.RequestCommon{}
			msg.Headers.MsgType = "TestAddInflightMsg"
			msg.Headers.ID = fmt.Sprintf("msg%d", i)
			msg1bytes, _ := json.Marshal(&msg)
			log.Infof("Sent message %d", i)
			mockConsumer.MockMessages <- &sarama.ConsumerMessage{Value: msg1bytes, Partition: 0, Offset: int64(i)}
		}
	}()

	// 10 messages should be sent into the processor
	var msgContexts []kldtx.TxnContext
	for i := 0; i < 10; i++ {
		msgContext := <-processor.messages
		log.Infof("Processor passed %s", msgContext.Headers().ID)
		assert.Equal(fmt.Sprintf("msg%d", i), msgContext.Headers().ID)
		msgContexts = append(msgContexts, msgContext)
	}
	assert.Equal(10, len(k.inFlight))

	// Send the replies for the first 10
	go func(msgContexts []kldtx.TxnContext) {
		for _, msgContext := range msgContexts {
			reply := kldmessages.ReplyCommon{}
			msgContext.Reply(&reply)
			log.Infof("Sent reply for %s", msgContext.Headers().ID)
		}
	}(msgContexts)
	// Drain the producer
	for i := 0; i < 10; i++ {
		msg := <-mockProducer.MockInput
		mockProducer.MockSuccesses <- msg
	}

	// 10 more messages should be sent into the processor
	msgContexts = []kldtx.TxnContext{}
	for i := 10; i < 20; i++ {
		msgContext := <-processor.messages
		log.Infof("Processor passed %s", msgContext.Headers().ID)
		assert.Equal(fmt.Sprintf("msg%d", i), msgContext.Headers().ID)
		msgContexts = append(msgContexts, msgContext)
	}
	assert.Equal(10, len(k.inFlight))

	// Send the replies for the next 10 - in reverse order
	var wg1 sync.WaitGroup
	wg1.Add(1)
	go func(msgContexts []kldtx.TxnContext) {
		for i := (len(msgContexts) - 1); i >= 0; i-- {
			msgContext := msgContexts[i]
			reply := kldmessages.ReplyCommon{}
			msgContext.Reply(&reply)
			log.Infof("Sent reply for %s", msgContext.Headers().ID)
		}
		wg1.Done()
	}(msgContexts)
	// Drain the producer
	for i := 0; i < 10; i++ {
		msg := <-mockProducer.MockInput
		mockProducer.MockSuccesses <- msg
	}
	wg1.Wait()

	// Shut down
	mockProducer.AsyncClose()
	mockConsumer.Close()
	wg.Wait()

	// Check we acknowledge all offsets
	assert.Equal(int64(19), mockConsumer.OffsetsByPartition[0])

}

func TestAddInflightDuplicateMessage(t *testing.T) {
	assert := assert.New(t)

	k, _, mockConsumer, mockProducer, wg := setupMocks()

	k.addInflightMsg(&sarama.ConsumerMessage{
		Value:     []byte("first"),
		Partition: 64,
		Offset:    int64(42),
		Topic:     "test",
	}, mockProducer)

	reqOffset := "test:64:42"

	firstMessage, exists := k.inFlight[reqOffset]
	assert.True(exists)

	k.addInflightMsg(&sarama.ConsumerMessage{
		Value:     []byte("duplicate offset"),
		Partition: 64,
		Offset:    int64(42),
		Topic:     "test",
	}, mockProducer)

	assert.Equal(firstMessage, k.inFlight[reqOffset])

	// Shut down
	mockProducer.AsyncClose()
	mockConsumer.Close()
	wg.Wait()

}

func TestAddInflightMessageBadMessage(t *testing.T) {
	assert := assert.New(t)

	k, _, mockConsumer, mockProducer, wg := setupMocks()

	mockConsumer.MockMessages <- &sarama.ConsumerMessage{
		Value:     []byte("badness"),
		Partition: 64,
		Offset:    int64(42),
		Topic:     "test",
	}

	// Drain the producer
	msg := <-mockProducer.MockInput
	for exists := false; !exists; _, exists = k.inFlight["test:64:42"] {
		time.Sleep(1 * time.Millisecond)
	}

	mockProducer.MockSuccesses <- msg

	for mockConsumer.OffsetsByPartition[64] != 42 {
		time.Sleep(1 * time.Millisecond)
	}

	// Shut down
	mockProducer.AsyncClose()
	mockConsumer.Close()
	wg.Wait()

	// Check we acknowledge all offsets
	assert.Equal(int64(42), mockConsumer.OffsetsByPartition[64])
}

func TestProducerErrorLoopPanics(t *testing.T) {
	assert := assert.New(t)

	k, _, mockConsumer, mockProducer, wg := setupMocks()

	wg.Add(2)
	go func() {
		mockProducer.MockErrors <- &sarama.ProducerError{
			Err: fmt.Errorf("pop"),
			Msg: &sarama.ProducerMessage{},
		}
		wg.Done()
	}()

	assert.Panics(func() {
		k.ProducerErrorLoop(nil, mockProducer, wg)
	})

	mockProducer.AsyncClose()
	mockConsumer.Close()
	wg.Wait()

}

func TestProducerSuccessLoopPanicsMsgNotInflight(t *testing.T) {
	assert := assert.New(t)

	k, _, mockConsumer, mockProducer, wg := setupMocks()

	wg.Add(2)
	go func() {
		mockProducer.MockSuccesses <- &sarama.ProducerMessage{
			Metadata: "badness",
		}
		wg.Done()
	}()

	assert.Panics(func() {
		k.ProducerSuccessLoop(nil, mockProducer, wg)
	})

	mockProducer.AsyncClose()
	mockConsumer.Close()
	wg.Wait()

}
