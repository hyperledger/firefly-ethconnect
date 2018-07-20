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
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/kaleido-io/ethconnect/internal/kldkafka"
	"github.com/kaleido-io/ethconnect/internal/kldmessages"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

type testKafkaCommon struct {
	stop            chan bool
	startCalled     bool
	startErr        error
	cobraInitCalled bool
	validateConfErr error
	kafkaFactory    *kldkafka.MockKafkaFactory
	kafkaInitDelay  int
	startTime       time.Time
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

func (k *testKafkaCommon) Stop() {
	k.stop <- true
}

func (k *testKafkaCommon) CobraInit(cmd *cobra.Command) {
	k.cobraInitCalled = true
}

func (k *testKafkaCommon) ValidateConf() error {
	return k.validateConfErr
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
		producer = k.kafkaFactory.Producer
	}
	return producer
}

var webhookExecuteError atomic.Value

var lastPort = 9000

func newTestKafkaComon() *testKafkaCommon {
	log.SetLevel(log.DebugLevel)
	kafka := &testKafkaCommon{}
	kafka.startTime = time.Now()
	kafka.stop = make(chan bool)
	kafka.kafkaFactory = kldkafka.NewMockKafkaFactory()
	kafka.kafkaFactory.NewProducer(kafka)
	kafka.kafkaFactory.NewConsumer(kafka)
	return kafka
}

// startTestWebhooks creates a Webhooks instance with a Cobra command wrapper, and executes it
// It returns once it's reached kafka initialization successfully, or errored during initialization
func startTestWebhooks(testArgs []string, kafka *testKafkaCommon) (*WebhooksBridge, error) {
	log.SetLevel(log.DebugLevel)
	w := NewWebhooksBridge()
	w.kafka = kafka
	cmd := w.CobraInit()
	if testArgs == nil {
		testArgs = []string{"-l", strconv.Itoa(lastPort)}
		lastPort++
	}
	cmd.SetArgs(testArgs)
	webhookExecuteError.Store(errors.New("none"))
	go func(cmd *cobra.Command) {
		err := cmd.Execute()
		log.Infof("Kafka webhooks completed. Err=%s", err)
		if err != nil {
			webhookExecuteError.Store(err)
		}
	}(cmd)
	status := -1
	startTime := time.Now()
	err := webhookExecuteError.Load().(error)
	for status != 200 && err.Error() == "none" && time.Now().Sub(startTime) < (4*time.Second) {
		time.Sleep(50 * time.Millisecond)
		statusURL := fmt.Sprintf("http://localhost:%d/status", w.conf.HTTP.Port)
		resp, httpErr := http.Get(statusURL)
		if httpErr == nil {
			status = resp.StatusCode
		}
		err = webhookExecuteError.Load().(error)
		log.Infof("Waiting for Webhook server to start (URL=%s Status=%d HTTPErr=%s Err=%s)", statusURL, status, httpErr, err)
	}
	if err.Error() == "none" {
		err = nil
	}
	return w, err
}

func TestNewWebhooksBridge(t *testing.T) {
	assert := assert.New(t)
	w := NewWebhooksBridge()
	var conf WebhooksBridgeConf
	conf.HTTP.LocalAddr = "127.0.0.1"
	w.SetConf(&conf)
	assert.Equal("127.0.0.1", w.Conf().HTTP.LocalAddr)
}

func TestValidateConfInvalidArgs(t *testing.T) {
	assert := assert.New(t)
	w := NewWebhooksBridge()
	w.conf.MongoDB.URL = "mongodb://localhost:27017"
	err := w.ValidateConf()
	assert.Regexp("MongoDB URL, Database and Collection name must be specified to enable the receipt store", err.Error())
}

func TestStartStopDefaultArgs(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	port := lastPort
	w, err := startTestWebhooks(nil, k)
	assert.Nil(err)

	assert.Equal(port, w.conf.HTTP.Port)    // default
	assert.Equal("", w.conf.HTTP.LocalAddr) // default

	w.Stop()
}

func TestStartStopCustomArgs(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	w, err := startTestWebhooks([]string{"-l", "8081", "-L", "127.0.0.1"}, k)
	assert.Nil(err)

	assert.Equal(8081, w.conf.HTTP.Port)
	assert.Equal("127.0.0.1", w.conf.HTTP.LocalAddr)
	assert.Equal("127.0.0.1:8081", w.srv.Addr)

	w.Stop()
}

func TestStartStopKafkaInitDelay(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	k.kafkaInitDelay = 500
	w, err := startTestWebhooks(nil, k)
	assert.Nil(err)

	w.Stop()
}

func TestStartStopKafkaPreRunError(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	k.validateConfErr = fmt.Errorf("pop")
	_, err := startTestWebhooks(nil, k)
	assert.Errorf(err, "pop")
}

func assertSentResp(assert *assert.Assertions, resp *http.Response, ack bool) {
	assert.NotNil(resp)
	if resp == nil {
		return
	}
	assert.Equal(200, resp.StatusCode)
	replyBytes, err := ioutil.ReadAll(resp.Body)
	assert.Nil(err)
	if resp.StatusCode == 200 {
		var replyMsg sentMsg
		err = json.Unmarshal(replyBytes, &replyMsg)
		assert.Nil(err)
		assert.Equal(true, replyMsg.Sent)
		assert.NotEmpty(replyMsg.Request)
		if ack {
			assert.NotEmpty(replyMsg.Msg)
		}
	} else {
		var replyMsg errMsg
		err = json.Unmarshal(replyBytes, &replyMsg)
		assert.Nil(err)
		log.Errorf("Error from server: %s", replyMsg.Message)
	}
}

func assertOKResp(assert *assert.Assertions, resp *http.Response) {
	assert.Equal(200, resp.StatusCode)
	replyBytes, err := ioutil.ReadAll(resp.Body)
	assert.Nil(err)
	if resp.StatusCode == 200 {
		var replyMsg okMsg
		err = json.Unmarshal(replyBytes, &replyMsg)
		assert.Nil(err)
		assert.Equal(true, replyMsg.OK)
	} else {
		var replyMsg errMsg
		err = json.Unmarshal(replyBytes, &replyMsg)
		assert.Nil(err)
		log.Errorf("Error from server: %s", replyMsg.Message)
	}
}

func assertErrResp(assert *assert.Assertions, resp *http.Response, status int, msg string) {
	assert.Equal(status, resp.StatusCode)
	replyBytes, err := ioutil.ReadAll(resp.Body)
	assert.Nil(err)
	var replyMsg errMsg
	err = json.Unmarshal(replyBytes, &replyMsg)
	assert.Nil(err)
	assert.Regexp(msg, replyMsg.Message)
}

func sendTestTransaction(assert *assert.Assertions, msgBytes []byte, contentType string, sendErr error, ack bool) (*http.Response, [][]byte) {

	log.SetLevel(log.DebugLevel)

	k := newTestKafkaComon()
	w, err := startTestWebhooks(nil, k)
	assert.Nil(err)

	wg := &sync.WaitGroup{}
	wg.Add(3)
	var msgs [][]byte
	go func() {
		for msg := range k.kafkaFactory.Producer.MockInput {
			msgBytes, _ := msg.Value.Encode()
			log.Infof("Message sent by webhook bridge: %s", string(msgBytes))
			msgs = append(msgs, msgBytes)

			// Send an ack or an err
			k.kafkaFactory.Producer.CloseSync.Lock()
			if !k.kafkaFactory.Producer.Closed {
				if sendErr != nil {
					k.kafkaFactory.Producer.MockErrors <- &sarama.ProducerError{
						Msg: msg,
						Err: sendErr,
					}
				} else {
					k.kafkaFactory.Producer.MockSuccesses <- msg
				}
			}
			k.kafkaFactory.Producer.CloseSync.Unlock()
		}
		wg.Done()
	}()

	go w.ProducerSuccessLoop(k.kafkaFactory.Consumer, k.kafkaFactory.Producer, wg)
	go w.ProducerErrorLoop(k.kafkaFactory.Consumer, k.kafkaFactory.Producer, wg)

	var url string
	if ack {
		url = fmt.Sprintf("http://localhost:%d/hook", w.conf.HTTP.Port)
	} else {
		url = fmt.Sprintf("http://localhost:%d/fasthook", w.conf.HTTP.Port)

	}
	resp, httpErr := http.Post(url, contentType, bytes.NewReader(msgBytes))
	if err != nil {
		log.Errorf("HTTP error for %s: %+v", url, err)
	}
	assert.Nil(httpErr)

	w.Stop()
	wg.Wait()

	return resp, msgs
}

func TestWebhookHandlerJSONSendTransaction(t *testing.T) {
	assert := assert.New(t)

	msg := kldmessages.SendTransaction{}
	msg.Headers.MsgType = kldmessages.MsgTypeSendTransaction
	msgBytes, _ := json.Marshal(&msg)
	resp, replyMsgs := sendTestTransaction(assert, msgBytes, "application/json", nil, true)
	assertSentResp(assert, resp, true)
	assert.Equal(1, len(replyMsgs))

	forwardedMessage := kldmessages.SendTransaction{}
	json.Unmarshal(replyMsgs[0], &forwardedMessage)
	assert.Equal(kldmessages.MsgTypeSendTransaction, forwardedMessage.Headers.MsgType)
}

func TestWebhookHandlerJSONSendTransactionNoAck(t *testing.T) {

	assert := assert.New(t)

	msg := kldmessages.SendTransaction{}
	msg.Headers.MsgType = kldmessages.MsgTypeSendTransaction
	msgBytes, _ := json.Marshal(&msg)
	resp, replyMsgs := sendTestTransaction(assert, msgBytes, "application/json", nil, false)
	assertSentResp(assert, resp, false)
	assert.Equal(1, len(replyMsgs))

	forwardedMessage := kldmessages.SendTransaction{}
	json.Unmarshal(replyMsgs[0], &forwardedMessage)
	assert.Equal(kldmessages.MsgTypeSendTransaction, forwardedMessage.Headers.MsgType)
}

func TestWebhookHandlerJSONSendFailedToKafka(t *testing.T) {

	assert := assert.New(t)

	msg := kldmessages.SendTransaction{}
	msg.Headers.MsgType = kldmessages.MsgTypeSendTransaction
	msgBytes, _ := json.Marshal(&msg)
	resp, _ := sendTestTransaction(assert, msgBytes, "application/json", fmt.Errorf("pop"), true)
	assertErrResp(assert, resp, 502, "Failed to deliver message to Kafka.*pop")
}

func TestWebhookHandlerJSONSendFailedToKafkaNoAck(t *testing.T) {

	assert := assert.New(t)

	msg := kldmessages.SendTransaction{}
	msg.Headers.MsgType = kldmessages.MsgTypeSendTransaction
	msgBytes, _ := json.Marshal(&msg)
	resp, replyMsgs := sendTestTransaction(assert, msgBytes, "application/json", fmt.Errorf("pop"), false)
	// Error is swallowed
	assertSentResp(assert, resp, false)
	assert.Equal(1, len(replyMsgs))

	forwardedMessage := kldmessages.SendTransaction{}
	json.Unmarshal(replyMsgs[0], &forwardedMessage)
	assert.Equal(kldmessages.MsgTypeSendTransaction, forwardedMessage.Headers.MsgType)
}

func TestProducerErrorLoopPanicsOnBadErrStructure(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	w, err := startTestWebhooks(nil, k)
	assert.Nil(err)

	go func() {
		k.kafkaFactory.Producer.MockErrors <- &sarama.ProducerError{
			Err: nil,
			Msg: nil,
		}
	}()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	assert.Panics(func() {
		defer wg.Done()
		w.ProducerErrorLoop(k.kafkaFactory.Consumer, k.kafkaFactory.Producer, wg)
	})

	wg.Wait()
	w.Stop()
}

func TestProducerErrorLoopPanicsOnBadMsgStructure(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	w, err := startTestWebhooks(nil, k)
	assert.Nil(err)

	go func() {
		k.kafkaFactory.Producer.MockSuccesses <- &sarama.ProducerMessage{
			Metadata: nil,
		}
	}()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	assert.Panics(func() {
		defer wg.Done()
		w.ProducerSuccessLoop(k.kafkaFactory.Consumer, k.kafkaFactory.Producer, wg)
	})

	wg.Wait()
	w.Stop()
}

func TestWebhookHandlerJSONDeployContract(t *testing.T) {

	assert := assert.New(t)

	msg := kldmessages.DeployContract{}
	msg.Headers.MsgType = kldmessages.MsgTypeDeployContract
	msg.From = "any string"
	msgBytes, _ := json.Marshal(&msg)
	resp, replyMsgs := sendTestTransaction(assert, msgBytes, "application/json", nil, true)
	assertSentResp(assert, resp, true)
	assert.Equal(1, len(replyMsgs))

	forwardedMessage := kldmessages.SendTransaction{}
	json.Unmarshal(replyMsgs[0], &forwardedMessage)
	assert.Equal(kldmessages.MsgTypeDeployContract, forwardedMessage.Headers.MsgType)
}

func TestWebhookHandlerYAMLDeployContract(t *testing.T) {

	assert := assert.New(t)

	msg := "" +
		"headers:\n" +
		"  type: DeployContract\n" +
		"from: '0x4b098809E68C88e26442491c57866b7D4852216c'\n" +
		"solidity: |-\n" +
		"  pragma solidity ^0.4.17;\n" +
		"  \n" +
		"  contract simplestorage {\n" +
		"    uint public storedData;\n" +
		"  \n" +
		"    function simplestorage(uint initVal) public {\n" +
		"      storedData = initVal;\n" +
		"    }\n" +
		"\n" +
		"    function set(uint x) public {\n" +
		"      storedData = x;\n" +
		"    }\n" +
		"    \n" +
		"    function get() public constant returns (uint retVal) {\n" +
		"      return storedData;\n" +
		"    }\n" +
		"  }\n" +
		"\n"

	resp, replyMsgs := sendTestTransaction(assert, []byte(msg), "application/x-yaml", nil, true)
	assertSentResp(assert, resp, true)
	assert.Equal(1, len(replyMsgs))

	forwardedMessage := kldmessages.SendTransaction{}
	json.Unmarshal(replyMsgs[0], &forwardedMessage)
	assert.Equal(kldmessages.MsgTypeDeployContract, forwardedMessage.Headers.MsgType)
}

func TestWebhookHandlerYAMLBadHeaders(t *testing.T) {

	assert := assert.New(t)

	msg := "" +
		"headers: some string" +
		"\n"

	resp, replyMsgs := sendTestTransaction(assert, []byte(msg), "application/x-yaml", nil, true)
	assertErrResp(assert, resp, 400, "Invalid message - missing 'headers' \\(or not an object\\)")
	assert.Equal(0, len(replyMsgs))
}

func TestWebhookHandlerYAMLMissingType(t *testing.T) {

	assert := assert.New(t)

	msg := "" +
		"headers:\n" +
		"  type:\n" +
		"    an: object\n" +
		"\n"

	resp, replyMsgs := sendTestTransaction(assert, []byte(msg), "application/x-yaml", nil, true)
	assertErrResp(assert, resp, 400, "Invalid message - missing 'headers.type' \\(or not a string\\)")
	assert.Equal(0, len(replyMsgs))
}

func TestWebhookHandlerYAMLMissingTo(t *testing.T) {

	assert := assert.New(t)

	msg := "" +
		"headers:\n" +
		"  type: DeployContract\n" +
		"\n"

	resp, replyMsgs := sendTestTransaction(assert, []byte(msg), "application/x-yaml", nil, true)
	assertErrResp(assert, resp, 400, "Invalid message - missing 'from' \\(or not a string\\)")
	assert.Equal(0, len(replyMsgs))
}

func TestWebhookHandlerBadYAML(t *testing.T) {

	assert := assert.New(t)

	resp, replyMsgs := sendTestTransaction(assert, []byte("!badness!"), "application/x-yaml", nil, true)
	assertErrResp(assert, resp, 400, "Unable to parse YAML")
	assert.Equal(0, len(replyMsgs))
}

func TestWebhookHandlerBadJSON(t *testing.T) {

	assert := assert.New(t)

	resp, replyMsgs := sendTestTransaction(assert, []byte("badness"), "application/json", nil, true)
	assertErrResp(assert, resp, 400, "Unable to parse JSON")
	assert.Equal(0, len(replyMsgs))
}

func TestWebhookHandlerBadMsgType(t *testing.T) {

	assert := assert.New(t)

	msg := kldmessages.RequestCommon{}
	msg.Headers.MsgType = "badness"
	msgBytes, _ := json.Marshal(&msg)
	resp, replyMsgs := sendTestTransaction(assert, msgBytes, "application/json", nil, true)
	assertErrResp(assert, resp, 400, "Invalid message type")
	assert.Equal(0, len(replyMsgs))
}

func TestWebhookHandlerTooBig(t *testing.T) {

	assert := assert.New(t)

	// Build a 1MB payload
	msgBytes := make([]byte, 1024*1024)
	resp, replyMsgs := sendTestTransaction(assert, msgBytes, "application/json", nil, true)
	assertErrResp(assert, resp, 400, "Message exceeds maximum allowable size")
	assert.Equal(0, len(replyMsgs))
}

func TestConsumerMessagesLoopCallsReplyProcessorWithEmptyPayload(t *testing.T) {
	assert := assert.New(t)

	k := newTestKafkaComon()
	w, err := startTestWebhooks(nil, k)
	assert.Nil(err)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	consumer, _ := k.kafkaFactory.NewConsumer(k)
	producer, _ := k.kafkaFactory.NewProducer(k)

	go func() {
		w.ConsumerMessagesLoop(consumer, producer, wg)
	}()

	consumer.(*kldkafka.MockKafkaConsumer).MockMessages <- &sarama.ConsumerMessage{
		Value: []byte(""),
	}

	w.Stop()
	wg.Wait()

}
