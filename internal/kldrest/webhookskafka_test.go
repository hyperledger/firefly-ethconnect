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

package kldrest

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldauth"
	"github.com/kaleido-io/ethconnect/internal/kldauth/kldauthtest"
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

func newTestKafkaComon() *testKafkaCommon {
	log.SetLevel(log.DebugLevel)
	kafka := &testKafkaCommon{}
	kafka.startTime = time.Now().UTC()
	kafka.stop = make(chan bool)
	kafka.kafkaFactory = kldkafka.NewMockKafkaFactory()
	kafka.kafkaFactory.NewProducer(kafka)
	kafka.kafkaFactory.NewConsumer(kafka)
	return kafka
}

func newTestWebhooks() (*webhooks, *webhooksKafka, *testKafkaCommon, *httptest.Server) {
	p := &memoryReceipts{}
	r := newReceiptStore(&ReceiptStoreConf{}, p, nil)
	k := newTestKafkaComon()
	wk := newWebhooksKafkaBase(r)
	wk.kafka = k
	w := newWebhooks(wk, nil)
	router := &httprouter.Router{}
	w.addRoutes(router)
	ts := httptest.NewUnstartedServer(router)
	ts.Config.BaseContext = func(l net.Listener) context.Context {
		ctx, _ := kldauth.WithAuthContext(context.Background(), "testat")
		return ctx
	}
	ts.Start()
	return w, wk, k, ts
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
		var replyMsg kldmessages.AsyncSentMsg
		err = json.Unmarshal(replyBytes, &replyMsg)
		assert.Nil(err)
		assert.Equal(true, replyMsg.Sent)
		assert.NotEmpty(replyMsg.Request)
		if ack {
			assert.NotEmpty(replyMsg.Msg)
		}
	} else {
		var replyMsg restError
		err = json.Unmarshal(replyBytes, &replyMsg)
		assert.Nil(err)
		log.Errorf("Error from server: %s", replyMsg.Message)
	}
}

func assertErrResp(assert *assert.Assertions, resp *http.Response, status int, msg string) {
	assert.Equal(status, resp.StatusCode)
	replyBytes, err := ioutil.ReadAll(resp.Body)
	assert.Nil(err)
	var replyMsg restError
	err = json.Unmarshal(replyBytes, &replyMsg)
	assert.Nil(err)
	assert.Regexp(msg, replyMsg.Message)
}

func sendTestTransaction(assert *assert.Assertions, msgBytes []byte, contentType string, sendErr error, ack bool) (*http.Response, [][]byte) {

	log.SetLevel(log.DebugLevel)
	_, wk, k, ts := newTestWebhooks()
	defer ts.Close()
	go k.Start()

	wg := &sync.WaitGroup{}
	wg.Add(3)
	var msgs [][]byte
	go func() {
		for msg := range k.kafkaFactory.Producer.MockInput {
			msgBytes, _ := msg.Value.Encode()
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

	go wk.ProducerSuccessLoop(k.kafkaFactory.Consumer, k.kafkaFactory.Producer, wg)
	go wk.ProducerErrorLoop(k.kafkaFactory.Consumer, k.kafkaFactory.Producer, wg)

	var url *url.URL
	if ack {
		url, _ = url.Parse(fmt.Sprintf("%s/hook", ts.URL))
	} else {
		url, _ = url.Parse(fmt.Sprintf("%s/fasthook", ts.URL))
	}
	req := &http.Request{
		URL:    url,
		Method: http.MethodPost,
		Header: http.Header{
			"Content-Type": []string{contentType},
		},
		ContentLength: int64(len(msgBytes)),
		Body:          ioutil.NopCloser(bytes.NewReader(msgBytes)),
	}
	resp, httpErr := http.DefaultClient.Do(req)
	if httpErr != nil {
		log.Errorf("HTTP error for %s: %+v", url, httpErr)
	}
	assert.Nil(httpErr)

	k.stop <- true
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
	assert.NotEmpty(forwardedMessage.Headers.ID)
}

func TestWebhookHandlerJSONSendnWithAccessToken(t *testing.T) {

	kldauth.RegisterSecurityModule(&kldauthtest.TestSecurityModule{})
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
	assert.NotEmpty(forwardedMessage.Headers.ID)

	kldauth.RegisterSecurityModule(nil)

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

	log.SetLevel(log.DebugLevel)
	_, wk, k, ts := newTestWebhooks()
	defer ts.Close()
	go k.Start()

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
		wk.ProducerErrorLoop(k.kafkaFactory.Consumer, k.kafkaFactory.Producer, wg)
	})

	wg.Wait()
	k.stop <- true
}

func TestProducerErrorLoopPanicsOnBadMsgStructure(t *testing.T) {
	assert := assert.New(t)

	log.SetLevel(log.DebugLevel)
	_, wk, k, ts := newTestWebhooks()
	defer ts.Close()
	go k.Start()

	go func() {
		k.kafkaFactory.Producer.MockSuccesses <- &sarama.ProducerMessage{
			Metadata: nil,
		}
	}()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	assert.Panics(func() {
		defer wg.Done()
		wk.ProducerSuccessLoop(k.kafkaFactory.Consumer, k.kafkaFactory.Producer, wg)
	})

	wg.Wait()
	k.stop <- true
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
		"  pragma solidity >=0.4.22 <0.6.0;\n" +
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
	assertErrResp(assert, resp, 400, "Unable to parse as YAML or JSON")
	assert.Equal(0, len(replyMsgs))
}

func TestWebhookHandlerBadJSON(t *testing.T) {

	assert := assert.New(t)

	resp, replyMsgs := sendTestTransaction(assert, []byte("badness"), "application/json", nil, true)
	assertErrResp(assert, resp, 400, "Unable to parse as YAML or JSON")
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
	msgBytes := make([]byte, 1025*1024)
	resp, replyMsgs := sendTestTransaction(assert, msgBytes, "application/json", nil, true)
	assertErrResp(assert, resp, 400, "Message exceeds maximum allowable size")
	assert.Equal(0, len(replyMsgs))
}

func TestConsumerMessagesLoopCallsReplyProcessorWithEmptyPayload(t *testing.T) {
	assert := assert.New(t)

	log.SetLevel(log.DebugLevel)
	_, wk, k, ts := newTestWebhooks()
	defer ts.Close()
	go k.Start()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	consumer, _ := k.kafkaFactory.NewConsumer(k)
	producer, _ := k.kafkaFactory.NewProducer(k)

	go func() {
		wk.ConsumerMessagesLoop(consumer, producer, wg)
	}()

	consumer.(*kldkafka.MockKafkaConsumer).MockMessages <- &sarama.ConsumerMessage{
		Partition: 3,
		Offset:    12345,
		Value:     []byte(""),
	}

	k.stop <- true
	wg.Wait()

	assert.Equal(int64(12345), consumer.(*kldkafka.MockKafkaConsumer).OffsetsByPartition[3])

}
