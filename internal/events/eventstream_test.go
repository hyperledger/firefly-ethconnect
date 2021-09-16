// Copyright 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package events

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hyperledger-labs/firefly-ethconnect/internal/errors"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/ethbind"
	"github.com/hyperledger-labs/firefly-ethconnect/internal/kvstore"
	"github.com/hyperledger-labs/firefly-ethconnect/mocks/ethmocks"
	ethbinding "github.com/kaleido-io/ethbinding/pkg"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestConstructorNoSpec(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(newTestSubscriptionManager(), nil, nil)
	assert.EqualError(err, "No ID")
}

func TestConstructorBadType(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(newTestSubscriptionManager(), &StreamInfo{
		ID:   "123",
		Type: "random",
	}, nil)
	assert.EqualError(err, "Unknown action type 'random'")
}

func TestConstructorMissingWebhook(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(newTestSubscriptionManager(), &StreamInfo{
		ID:   "123",
		Type: "webhook",
	}, nil)
	assert.EqualError(err, "Must specify webhook.url for action type 'webhook'")
}

func TestConstructorBadWebhookURL(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(newTestSubscriptionManager(), &StreamInfo{
		ID:   "123",
		Type: "webhook",
		Webhook: &webhookActionInfo{
			URL: ":badurl",
		},
	}, nil)
	assert.EqualError(err, "Invalid URL in webhook action")
}

func TestConstructorBadWebSocketDistributionMode(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(newTestSubscriptionManager(), &StreamInfo{
		ID:   "123",
		Type: "websocket",
		WebSocket: &webSocketActionInfo{
			Topic:            "foobar",
			DistributionMode: "banana",
		},
	}, nil)
	assert.EqualError(err, "Invalid distribution mode 'banana'. Valid distribution modes are: 'workloadDistribution' and 'broadcast'.")
}

func testEvent(subID string) *eventData {
	return &eventData{
		SubID:         subID,
		batchComplete: func(*eventData) {},
	}
}

func newTestStreamForBatching(spec *StreamInfo, db kvstore.KVStore, status ...int) (*subscriptionMGR, *eventStream, *httptest.Server, chan []*eventData) {
	mux := http.NewServeMux()
	eventStream := make(chan []*eventData)
	count := 0
	mux.HandleFunc("/", func(res http.ResponseWriter, req *http.Request) {
		var events []*eventData
		json.NewDecoder(req.Body).Decode(&events)
		eventStream <- events
		idx := count
		if idx >= len(status) {
			idx = len(status) - 1
		}
		res.WriteHeader(status[idx])
		count++
	})
	svr := httptest.NewServer(mux)
	if spec.Type == "" {
		spec.Type = "WEBHOOK"
		spec.Webhook.URL = svr.URL
		spec.Webhook.Headers = map[string]string{"x-my-header": "my-value"}
	}
	sm := newTestSubscriptionManager()
	sm.config().WebhooksAllowPrivateIPs = true
	sm.config().EventPollingIntervalSec = 0
	if db != nil {
		sm.db = db
	}
	ctx := context.Background()
	stream, _ := sm.AddStream(ctx, spec)
	return sm, sm.streams[stream.ID], svr, eventStream
}

func newTestStreamForWebSocket(spec *StreamInfo, db kvstore.KVStore, status ...int) (*subscriptionMGR, *eventStream, *mockWebSocket) {
	sm := newTestSubscriptionManager()
	sm.config().EventPollingIntervalSec = 0
	if db != nil {
		sm.db = db
	}
	ctx := context.Background()
	stream, _ := sm.AddStream(ctx, spec)
	return sm, sm.streams[stream.ID], sm.wsChannels.(*mockWebSocket)
}

func TestBatchTimeout(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize:      10,
			BatchTimeoutMS: 50,
			Webhook:        &webhookActionInfo{},
		}, nil, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	var e1s []*eventData
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		e1s = <-eventStream
		wg.Done()
	}()
	for i := 0; i < 3; i++ {
		stream.handleEvent(testEvent(fmt.Sprintf("sub%d", i)))
	}
	wg.Wait()
	assert.Equal(3, len(e1s))

	var e2s, e3s []*eventData
	wg = sync.WaitGroup{}
	wg.Add(1)
	go func() {
		e2s = <-eventStream
		e3s = <-eventStream
		wg.Done()
	}()
	for i := 0; i < 19; i++ {
		stream.handleEvent(testEvent(fmt.Sprintf("sub%d", i)))
	}
	wg.Wait()
	assert.Equal(10, len(e2s))
	assert.Equal(9, len(e3s))
	for i := 0; i < 10 && stream.inFlight > 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(uint64(0), stream.inFlight)

}

func TestStopDuringTimeout(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize:      10,
			BatchTimeoutMS: 2000,
			Webhook:        &webhookActionInfo{},
		}, nil, 200)
	defer close(eventStream)
	defer svr.Close()

	stream.handleEvent(testEvent("sub1"))
	time.Sleep(10 * time.Millisecond)
	stream.stop()
	time.Sleep(10 * time.Millisecond)
	assert.True(stream.processorDone)
}

func TestBatchSizeCap(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize: 10000000,
			Webhook:   &webhookActionInfo{},
		}, nil, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	assert.Equal(uint64(MaxBatchSize), stream.spec.BatchSize)
	assert.Equal("", stream.spec.Name)
}

func TestStreamName(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			Name:    "testStream",
			Webhook: &webhookActionInfo{},
		}, nil, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	assert.Equal("testStream", stream.spec.Name)
}

func TestBlockingBehavior(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize:            1,
			Webhook:              &webhookActionInfo{},
			ErrorHandling:        ErrorHandlingBlock,
			BlockedRetryDelaySec: 1,
		}, nil, 404)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	complete := false
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() { <-eventStream; wg.Done() }()
	stream.handleEvent(&eventData{
		SubID:         "sub1",
		batchComplete: func(*eventData) { complete = true },
	})
	wg.Wait()
	time.Sleep(10 * time.Millisecond)
	assert.False(complete)
}

func TestSkippingBehavior(t *testing.T) {
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize:            1,
			Webhook:              &webhookActionInfo{},
			ErrorHandling:        ErrorHandlingSkip,
			BlockedRetryDelaySec: 1,
		}, nil, 404 /* fail the requests */)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	complete := false
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() { <-eventStream; wg.Done() }()
	stream.handleEvent(&eventData{
		SubID:         "sub1",
		batchComplete: func(*eventData) { complete = true },
	})
	wg.Wait()
	for !complete {
		time.Sleep(50 * time.Millisecond)
	}
	// reaching here despite the 404s means we passed
}

func TestBackoffRetry(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize:            1,
			Webhook:              &webhookActionInfo{},
			ErrorHandling:        ErrorHandlingBlock,
			RetryTimeoutSec:      1,
			BlockedRetryDelaySec: 1,
		}, nil, 404, 500, 503, 504, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()
	stream.initialRetryDelay = 1 * time.Millisecond
	stream.backoffFactor = 1.1

	complete := false
	thrown := false
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		for i := 0; i < 5; i++ {
			<-eventStream
			thrown = true
		}
		wg.Done()
	}()
	stream.handleEvent(&eventData{
		SubID:         "sub1",
		batchComplete: func(*eventData) { complete = true },
	})
	wg.Wait()
	assert.True(thrown)
	for !complete {
		time.Sleep(1 * time.Millisecond)
	}
}

func TestBlockedAddresses(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookActionInfo{},
		}, nil, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	stream.allowPrivateIPs = false

	complete := false
	go func() { <-eventStream }()
	stream.handleEvent(&eventData{
		SubID:         "sub1",
		batchComplete: func(*eventData) { complete = true },
	})
	time.Sleep(10 * time.Millisecond)
	assert.False(complete)
}

func TestBadDNSName(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingSkip,
			Webhook:       &webhookActionInfo{},
		}, nil, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()
	stream.spec.Webhook.URL = "http://fail.invalid"

	called := false
	complete := false
	go func() { <-eventStream; called = true }()
	stream.handleEvent(&eventData{
		SubID:         "sub1",
		batchComplete: func(*eventData) { complete = true },
	})
	for !complete {
		time.Sleep(1 * time.Millisecond)
	}
	assert.False(called)
}

func TestBuildup(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookActionInfo{},
		}, nil, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	assert.False(stream.isBlocked())

	// Hang the HTTP requests (no consumption from channel)
	for i := 0; i < 11; i++ {
		stream.handleEvent(testEvent(fmt.Sprintf("sub%d", i)))
	}

	for !stream.isBlocked() {
		time.Sleep(1 * time.Millisecond)
	}
	assert.True(stream.inFlight >= 10)

	for i := 0; i < 11; i++ {
		<-eventStream
	}
	for stream.isBlocked() {
		time.Sleep(1 * time.Millisecond)
	}
	assert.Equal(uint64(0), stream.inFlight)

}

func TestWebSocketUnconfigured(t *testing.T) {
	assert := assert.New(t)
	sm := NewSubscriptionManager(&SubscriptionManagerConf{}, nil, nil, nil).(*subscriptionMGR)
	_, err := sm.AddStream(context.Background(), &StreamInfo{Type: "websocket"})
	assert.EqualError(err, "WebSocket listener not configured")
}

func TestBadTimestampCacheSize(t *testing.T) {
	assert := assert.New(t)
	sm := NewSubscriptionManager(&SubscriptionManagerConf{}, nil, nil, nil).(*subscriptionMGR)
	_, err := sm.AddStream(context.Background(), &StreamInfo{
		TimestampCacheSize: -1,
	})
	assert.EqualError(err, "Failed to create a resource for the stream: must provide a positive size")
}

func setupTestSubscription(assert *assert.Assertions, sm *subscriptionMGR, stream *eventStream, subscriptionName string) *SubscriptionInfo {
	log.SetLevel(log.DebugLevel)
	testDataBytes, err := ioutil.ReadFile("../../test/simplevents_logs.json")
	assert.NoError(err)
	var testData []*logEntry
	json.Unmarshal(testDataBytes, &testData)
	testBlockDetailBytes, err := ioutil.ReadFile("../../test/block_details.json")
	assert.NoError(err)
	testBlock := &ethbinding.Header{}
	var parsedBlock map[string]interface{}
	json.Unmarshal(testBlockDetailBytes, &parsedBlock)
	ts := parsedBlock["timestamp"].(float64)
	testBlock.Time = uint64(ts)

	callCount := 0
	filterChangeCalls := 0
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			res := args[1]
			method := args[2].(string)
			callCount++
			log.Infof("UT %s call=%d", method, callCount)
			switch method {
			case "eth_blockNumber":
				// ignore
			case "eth_newFilter":
				// ignore
			case "eth_getFilterLogs":
				*(res.(*[]*logEntry)) = testData[0:2]
			case "eth_getFilterChanges":
				if filterChangeCalls == 0 {
					*(res.(*[]*logEntry)) = testData[2:]
				} else {
					*(res.(*[]*logEntry)) = []*logEntry{}
				}
				filterChangeCalls++
			case "eth_getBlockByNumber":
				*(res.(*ethbinding.Header)) = *testBlock
			}
		}).
		Return(nil)
	sm.rpc = rpc

	event := &ethbinding.ABIElementMarshaling{
		Name: "Changed",
		Inputs: []ethbinding.ABIArgumentMarshaling{
			{
				Name:    "from",
				Type:    "address",
				Indexed: true,
			},
			{
				Name:    "i",
				Type:    "int64",
				Indexed: true,
			},
			{
				Name:    "s",
				Type:    "string",
				Indexed: true,
			},
			{
				Name: "h",
				Type: "bytes32",
			},
			{
				Name: "m",
				Type: "string",
			},
		},
	}
	addr := ethbind.API.HexToAddress("0x167f57a13a9c35ff92f0649d2be0e52b4f8ac3ca")
	ctx := context.Background()
	s, _ := sm.AddSubscription(ctx, &addr, nil, event, stream.spec.ID, "", subscriptionName)
	return s
}

func setupCatchupTestSubscription(assert *assert.Assertions, sm *subscriptionMGR, stream *eventStream, subscriptionName string) *SubscriptionInfo {
	log.SetLevel(log.DebugLevel)
	testDataBytes, err := ioutil.ReadFile("../../test/simplevents_logs.json")
	assert.NoError(err)
	var testData []*logEntry
	json.Unmarshal(testDataBytes, &testData)
	testBlockDetailBytes, err := ioutil.ReadFile("../../test/block_details.json")
	assert.NoError(err)
	testBlock := &ethbinding.Header{}
	var parsedBlock map[string]interface{}
	json.Unmarshal(testBlockDetailBytes, &parsedBlock)
	ts := parsedBlock["timestamp"].(float64)
	testBlock.Time = uint64(ts)

	callCount := 0
	getLogsCalls := 0
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			res := args[1]
			method := args[2].(string)
			callCount++
			log.Infof("UT %s call=%d", method, callCount)
			switch method {
			case "eth_blockNumber":
				(*(res.(*ethbinding.HexBigInt))).ToInt().SetString("501", 10)
			case "eth_getLogs":
				if getLogsCalls == 0 {
					// Catchup page with data
					*(res.(*[]*logEntry)) = testData[0:2]
				} else {
					// Catchup page with no data
					*(res.(*[]*logEntry)) = []*logEntry{}
				}
				getLogsCalls++
				assert.LessOrEqual(getLogsCalls, 2)
			case "eth_getFilterLogs":
				// First page after catchup
				*(res.(*[]*logEntry)) = testData[2:]
			case "eth_getFilterChanges":
				// No further updates
				*(res.(*[]*logEntry)) = []*logEntry{}
			case "eth_getBlockByNumber":
				*(res.(*ethbinding.Header)) = *testBlock
			}
		}).
		Return(nil)
	sm.rpc = rpc

	event := &ethbinding.ABIElementMarshaling{
		Name: "Changed",
		Inputs: []ethbinding.ABIArgumentMarshaling{
			{
				Name:    "from",
				Type:    "address",
				Indexed: true,
			},
			{
				Name:    "i",
				Type:    "int64",
				Indexed: true,
			},
			{
				Name:    "s",
				Type:    "string",
				Indexed: true,
			},
			{
				Name: "h",
				Type: "bytes32",
			},
			{
				Name: "m",
				Type: "string",
			},
		},
	}
	addr := ethbind.API.HexToAddress("0x167f57a13a9c35ff92f0649d2be0e52b4f8ac3ca")
	ctx := context.Background()
	s, _ := sm.AddSubscription(ctx, &addr, nil, event, stream.spec.ID, "0", subscriptionName)
	return s
}

func TestProcessEventsEnd2EndWebhook(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize:  1,
			Webhook:    &webhookActionInfo{},
			Timestamps: false,
		}, db, 200)
	defer svr.Close()

	s := setupTestSubscription(assert, sm, stream, "mySubName")
	assert.Equal("mySubName", s.Name)

	// We expect three events to be sent to the webhook
	// With the default batch size of 1, that means three separate requests
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		e1s := <-eventStream
		assert.Equal(1, len(e1s))
		assert.Equal("42", e1s[0].Data["i"])
		assert.Equal("But what is the question?", e1s[0].Data["m"])
		assert.Equal("150665", e1s[0].BlockNumber)
		e2s := <-eventStream
		assert.Equal(1, len(e2s))
		assert.Equal("1977", e2s[0].Data["i"])
		assert.Equal("A long time ago in a galaxy far, far away....", e2s[0].Data["m"])
		assert.Equal("150665", e2s[0].BlockNumber)
		e3s := <-eventStream
		assert.Equal(1, len(e3s))
		assert.Equal("20151021", e3s[0].Data["i"])
		assert.Equal("1.21 Gigawatts!", e3s[0].Data["m"])
		assert.Equal("150721", e3s[0].BlockNumber)
		wg.Done()
	}()
	wg.Wait()

	ctx := context.Background()
	err := sm.DeleteSubscription(ctx, s.ID)
	assert.NoError(err)
	err = sm.DeleteStream(ctx, stream.spec.ID)
	assert.NoError(err)
	sm.Close()
}

func TestProcessEventsEnd2EndCatchupWebhook(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize:  1,
			Webhook:    &webhookActionInfo{},
			Timestamps: false,
		}, db, 200)
	defer svr.Close()

	s := setupCatchupTestSubscription(assert, sm, stream, "mySubName")
	assert.Equal("mySubName", s.Name)

	// We expect three events to be sent to the webhook
	// With the default batch size of 1, that means three separate requests
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		e1s := <-eventStream
		assert.Equal(1, len(e1s))
		assert.Equal("42", e1s[0].Data["i"])
		assert.Equal("But what is the question?", e1s[0].Data["m"])
		assert.Equal("150665", e1s[0].BlockNumber)
		e2s := <-eventStream
		assert.Equal(1, len(e2s))
		assert.Equal("1977", e2s[0].Data["i"])
		assert.Equal("A long time ago in a galaxy far, far away....", e2s[0].Data["m"])
		assert.Equal("150665", e2s[0].BlockNumber)
		e3s := <-eventStream
		assert.Equal(1, len(e3s))
		assert.Equal("20151021", e3s[0].Data["i"])
		assert.Equal("1.21 Gigawatts!", e3s[0].Data["m"])
		assert.Equal("150721", e3s[0].BlockNumber)
		wg.Done()
	}()
	wg.Wait()

	ctx := context.Background()
	err := sm.DeleteSubscription(ctx, s.ID)
	assert.NoError(err)
	err = sm.DeleteStream(ctx, stream.spec.ID)
	assert.NoError(err)
	sm.Close()
}

func TestProcessEventsEnd2EndWebSocket(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	sm, stream, mockWebSocket := newTestStreamForWebSocket(
		&StreamInfo{
			BatchSize:  1,
			Type:       "websocket",
			WebSocket:  &webSocketActionInfo{},
			Timestamps: false,
		}, db, 200)

	s := setupTestSubscription(assert, sm, stream, "mySubName")
	assert.Equal("mySubName", s.Name)

	// We expect three events to be sent to the webhook
	// With the default batch size of 1, that means three separate requests
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		e1s := (<-mockWebSocket.sender).([]*eventData)
		assert.Equal(1, len(e1s))
		assert.Equal("42", e1s[0].Data["i"])
		assert.Equal("But what is the question?", e1s[0].Data["m"])
		assert.Equal("150665", e1s[0].BlockNumber)
		mockWebSocket.receiver <- nil
		e2s := (<-mockWebSocket.sender).([]*eventData)
		assert.Equal(1, len(e2s))
		assert.Equal("1977", e2s[0].Data["i"])
		assert.Equal("A long time ago in a galaxy far, far away....", e2s[0].Data["m"])
		assert.Equal("150665", e2s[0].BlockNumber)
		mockWebSocket.receiver <- nil
		e3s := (<-mockWebSocket.sender).([]*eventData)
		assert.Equal(1, len(e3s))
		assert.Equal("20151021", e3s[0].Data["i"])
		assert.Equal("1.21 Gigawatts!", e3s[0].Data["m"])
		assert.Equal("150721", e3s[0].BlockNumber)
		mockWebSocket.receiver <- nil
		wg.Done()
	}()
	wg.Wait()

	ctx := context.Background()
	err := sm.DeleteSubscription(ctx, s.ID)
	assert.NoError(err)
	err = sm.DeleteStream(ctx, stream.spec.ID)
	assert.NoError(err)
	sm.Close()
}

func TestProcessEventsEnd2EndWithTimestamps(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize:  1,
			Webhook:    &webhookActionInfo{},
			Timestamps: true,
		}, db, 200)
	defer svr.Close()

	s := setupTestSubscription(assert, sm, stream, "mySubName")
	assert.Equal("mySubName", s.Name)

	// We expect three events to be sent to the webhook
	// With the default batch size of 1, that means three separate requests
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		e1s := <-eventStream
		assert.Equal(1, len(e1s))
		assert.Equal("42", e1s[0].Data["i"])
		assert.Equal("But what is the question?", e1s[0].Data["m"])
		assert.Equal("150665", e1s[0].BlockNumber)
		assert.Equal("1588748143", e1s[0].Timestamp)
		e2s := <-eventStream
		assert.Equal(1, len(e2s))
		assert.Equal("1977", e2s[0].Data["i"])
		assert.Equal("A long time ago in a galaxy far, far away....", e2s[0].Data["m"])
		assert.Equal("150665", e2s[0].BlockNumber)
		assert.Equal("1588748143", e2s[0].Timestamp)
		e3s := <-eventStream
		assert.Equal(1, len(e3s))
		assert.Equal("20151021", e3s[0].Data["i"])
		assert.Equal("1.21 Gigawatts!", e3s[0].Data["m"])
		assert.Equal("150721", e3s[0].BlockNumber)
		assert.Equal("1588748143", e3s[0].Timestamp)
		wg.Done()
	}()
	wg.Wait()

	ctx := context.Background()
	err := sm.DeleteSubscription(ctx, s.ID)
	assert.NoError(err)
	err = sm.DeleteStream(ctx, stream.spec.ID)
	assert.NoError(err)
	sm.Close()
}

func TestProcessEventsEnd2EndWithReset(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize:  1,
			Webhook:    &webhookActionInfo{},
			Timestamps: false,
		}, db, 200)
	defer svr.Close()

	s := setupTestSubscription(assert, sm, stream, "mySubName")
	assert.Equal("mySubName", s.Name)

	// We expect three events to be sent to the webhook
	// With the default batch size of 1, that means three separate requests
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		e1s := <-eventStream
		assert.Equal(1, len(e1s))
		assert.Equal("42", e1s[0].Data["i"])
		assert.Equal("But what is the question?", e1s[0].Data["m"])
		assert.Equal("150665", e1s[0].BlockNumber)
		e2s := <-eventStream
		assert.Equal(1, len(e2s))
		assert.Equal("1977", e2s[0].Data["i"])
		assert.Equal("A long time ago in a galaxy far, far away....", e2s[0].Data["m"])
		assert.Equal("150665", e2s[0].BlockNumber)
		e3s := <-eventStream
		assert.Equal(1, len(e3s))
		assert.Equal("20151021", e3s[0].Data["i"])
		assert.Equal("1.21 Gigawatts!", e3s[0].Data["m"])
		assert.Equal("150721", e3s[0].BlockNumber)
		wg.Done()
	}()
	wg.Wait()

	ctx := context.Background()
	err := sm.ResetSubscription(ctx, s.ID, "0")
	assert.NoError(err)

	wg = &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		e1s := <-eventStream
		assert.Equal(1, len(e1s))
		assert.Equal("42", e1s[0].Data["i"])
		assert.Equal("But what is the question?", e1s[0].Data["m"])
		assert.Equal("150665", e1s[0].BlockNumber)
		e2s := <-eventStream
		assert.Equal(1, len(e2s))
		assert.Equal("1977", e2s[0].Data["i"])
		assert.Equal("A long time ago in a galaxy far, far away....", e2s[0].Data["m"])
		assert.Equal("150665", e2s[0].BlockNumber)
		// Note no third event this time round, as eth_getFilterChanges test data is only set to fire on exactly one call
		wg.Done()
	}()
	wg.Wait()

	err = sm.DeleteSubscription(ctx, s.ID)
	assert.NoError(err)
	err = sm.DeleteStream(ctx, stream.spec.ID)
	assert.NoError(err)
	sm.Close()
}

func TestInterruptWebSocketBroadcast(t *testing.T) {
	wsChannels := &mockWebSocket{
		sender:   make(chan interface{}),
		receiver: make(chan error),
	}
	es := &eventStream{
		wsChannels:      wsChannels,
		updateInterrupt: make(chan struct{}),
	}
	sio, _ := newWebSocketAction(es, &webSocketActionInfo{
		DistributionMode: "broadcast",
	})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		close(es.updateInterrupt)
		wg.Done()
	}()
	sio.attemptBatch(0, 1, []*eventData{})
	wg.Wait()
}

func TestInterruptWebSocketSend(t *testing.T) {
	wsChannels := &mockWebSocket{
		sender:   make(chan interface{}),
		receiver: make(chan error),
	}
	es := &eventStream{
		wsChannels:      wsChannels,
		updateInterrupt: make(chan struct{}),
	}
	sio, _ := newWebSocketAction(es, &webSocketActionInfo{})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		close(es.updateInterrupt)
		wg.Done()
	}()
	sio.attemptBatch(0, 1, []*eventData{})
	wg.Wait()
}

func TestInterruptWebSocketReceive(t *testing.T) {
	wsChannels := &mockWebSocket{
		sender:    make(chan interface{}),
		broadcast: make(chan interface{}),
		receiver:  make(chan error),
		closing:   make(chan struct{}),
	}
	es := &eventStream{
		wsChannels:      wsChannels,
		updateInterrupt: make(chan struct{}),
	}
	sio, _ := newWebSocketAction(es, &webSocketActionInfo{})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		<-wsChannels.sender
		close(es.updateInterrupt)
		wg.Done()
	}()
	sio.attemptBatch(0, 1, []*eventData{})
	wg.Wait()
}

func TestCheckpointRecovery(t *testing.T) {
	assert := assert.New(t)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookActionInfo{},
		}, nil, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		for i := 0; i < 3; i++ {
			<-eventStream
		}
		wg.Done()
	}()

	s := setupTestSubscription(assert, sm, stream, "myTestSub")
	assert.Equal("myTestSub", s.Name)

	for {
		time.Sleep(1 * time.Millisecond)
		cp, err := sm.loadCheckpoint(stream.spec.ID)
		if err == nil {
			v, exists := cp[s.ID]
			t.Logf("Checkpoint? %t (%+v)", exists, v)
			if v != nil && big.NewInt(150722).Cmp(v) == 0 {
				break
			}
		}
	}
	stream.suspend()
	for !stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}

	// Restart from the checkpoint that was stored
	var newFilterBlock uint64
	sub := sm.subscriptions[s.ID]
	sub.filterStale = true
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			res := args[1]
			method := args[2].(string)
			if method == "eth_newFilter" {
				newFilterBlock = args[3].(*ethFilter).FromBlock.ToInt().Uint64()
				t.Logf("New filter block after checkpoint recovery: %d", newFilterBlock)
			} else if method == "eth_getFilterChanges" {
				*(res.(*[]*logEntry)) = []*logEntry{}
			} else if method == "eth_uninstallFilter" {
				*(res.(*bool)) = true
			}
		}).
		Return(nil)
	sub.rpc = rpc

	stream.resume()
	for stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}
	wg.Wait()
	for uint64(150722) != newFilterBlock {
		time.Sleep(1 * time.Millisecond)
	}

	rpc.AssertExpectations(t)
}

func TestWithoutCheckpointRecovery(t *testing.T) {
	assert := assert.New(t)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookActionInfo{},
		}, nil, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	stream.suspend()
	for !stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}

	s := setupTestSubscription(assert, sm, stream, "")

	var initialEndBlock string
	sub := sm.subscriptions[s.ID]
	sub.filterStale = true
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			res := args[1]
			method := args[2].(string)
			if method == "eth_blockNumber" {
			} else if method == "eth_newFilter" {
				initialEndBlock = args[3].(*ethFilter).ToBlock
				t.Logf("New filter block after recovery with no checkpoint: %s", initialEndBlock)
			} else if method == "eth_getFilterChanges" {
				*(res.(*[]*logEntry)) = []*logEntry{}
			} else if method == "eth_uninstallFilter" {
				*(res.(*bool)) = true
			}
		}).
		Return(nil)
	sub.rpc = rpc

	stream.resume()
	for stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}
	for initialEndBlock != "latest" {
		time.Sleep(1 * time.Millisecond)
	}

	rpc.AssertExpectations(t)
}

func TestMarkStaleOnError(t *testing.T) {
	assert := assert.New(t)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookActionInfo{},
		}, nil, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	stream.suspend()
	for !stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}

	s := setupTestSubscription(assert, sm, stream, "")
	sm.subscriptions[s.ID].filterStale = false

	sub := sm.subscriptions[s.ID]
	rpc := &ethmocks.RPCClient{}
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_getFilterLogs", mock.Anything).
		Return(fmt.Errorf("filter not found"))
	rpc.On("CallContext", mock.Anything, mock.Anything, "eth_uninstallFilter", mock.Anything).
		Return(nil)
	sub.rpc = rpc

	stream.resume()
	for stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}
	for !sub.filterStale {
		time.Sleep(1 * time.Millisecond)
	}

	rpc.AssertExpectations(t)
}

func TestStoreCheckpointLoadError(t *testing.T) {
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookActionInfo{},
		}, nil, 200)
	mockKV := kvstore.NewMockKV(fmt.Errorf("pop"))
	sm.db = mockKV
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	stream.suspend()
	for !stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}
	stream.resume()
	for stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}
}

func TestStoreCheckpointStoreError(t *testing.T) {
	assert := assert.New(t)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookActionInfo{},
		}, nil, 200)
	mockKV := kvstore.NewMockKV(nil)
	mockKV.StoreErr = fmt.Errorf("pop")
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		for i := 0; i < 3; i++ {
			<-eventStream
		}
		wg.Done()
	}()
	setupTestSubscription(assert, sm, stream, "")
	wg.Wait()

	stream.suspend()
	for !stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}
}

func TestProcessBatchEmptyArray(t *testing.T) {
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookActionInfo{},
		}, nil, 200)
	mockKV := kvstore.NewMockKV(nil)
	mockKV.StoreErr = fmt.Errorf("pop")
	sm.db = mockKV
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	stream.processBatch(0, []*eventData{})
}

func TestUpdateStream(t *testing.T) {
	// The test performs the following steps:
	// * Create a stream with batch size 5
	// * Push 3 events
	// * Update the event stream and verify updated fields
	// * close event stream
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			BatchSize:     5,
			Webhook:       &webhookActionInfo{},
		}, db, 200)
	defer svr.Close()
	defer close(eventStream)
	defer stream.stop()

	for i := 0; i < 3; i++ {
		stream.handleEvent(testEvent(fmt.Sprintf("sub%d", i)))
	}
	ctx := context.Background()
	headers := make(map[string]string)
	headers["test-h1"] = "val1"
	updateSpec := &StreamInfo{
		BatchSize:            4,
		BatchTimeoutMS:       10000,
		BlockedRetryDelaySec: 5,
		ErrorHandling:        ErrorHandlingBlock,
		Name:                 "new-name",
		Webhook: &webhookActionInfo{
			URL:               "http://foo.url",
			Headers:           headers,
			TLSkipHostVerify:  true,
			RequestTimeoutSec: 0,
		},
		Timestamps: true,
	}
	updatedStream, err := sm.UpdateStream(ctx, stream.spec.ID, updateSpec)
	assert.Equal(updatedStream.Name, "new-name")
	assert.Equal(updatedStream.Timestamps, true)
	assert.Equal(updatedStream.BatchSize, uint64(4))
	assert.Equal(updatedStream.BatchTimeoutMS, uint64(10000))
	assert.Equal(updatedStream.BlockedRetryDelaySec, uint64(5))
	assert.Equal(updatedStream.ErrorHandling, ErrorHandlingBlock)
	assert.Equal(updatedStream.Webhook.URL, "http://foo.url")
	assert.Equal(updatedStream.Webhook.Headers["test-h1"], "val1")

	assert.NoError(err)
}

func TestUpdateStreamSwapType(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			BatchSize:     5,
			Webhook:       &webhookActionInfo{},
		}, db, 200)
	defer svr.Close()
	defer close(eventStream)
	defer stream.stop()

	ctx := context.Background()
	updateSpec := &StreamInfo{
		Type: "websocket",
		WebSocket: &webSocketActionInfo{
			Topic: "test1",
		},
	}
	_, err := sm.UpdateStream(ctx, stream.spec.ID, updateSpec)
	assert.EqualError(err, "The type of an event stream cannot be changed")
}

func TestUpdateWebSocketBadDistributionMode(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			BatchSize:     5,
			Type:          "websocket",
			Name:          "websocket-stream",
			WebSocket: &webSocketActionInfo{
				Topic: "test1",
			},
		}, db, 200)
	defer svr.Close()
	defer close(eventStream)
	defer stream.stop()

	ctx := context.Background()
	updateSpec := &StreamInfo{
		WebSocket: &webSocketActionInfo{
			Topic:            "test2",
			DistributionMode: "banana",
		},
	}
	_, err := sm.UpdateStream(ctx, stream.spec.ID, updateSpec)
	assert.EqualError(err, "Invalid distribution mode 'banana'. Valid distribution modes are: 'workloadDistribution' and 'broadcast'.")
}

func TestUpdateWebSocket(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			BatchSize:     5,
			Type:          "websocket",
			Name:          "websocket-stream",
			WebSocket: &webSocketActionInfo{
				Topic: "test1",
			},
		}, db, 200)
	defer svr.Close()
	defer close(eventStream)
	defer stream.stop()

	ctx := context.Background()
	updateSpec := &StreamInfo{
		WebSocket: &webSocketActionInfo{
			Topic: "test2",
		},
	}
	updatedStream, err := sm.UpdateStream(ctx, stream.spec.ID, updateSpec)
	assert.Equal("test2", updatedStream.WebSocket.Topic)
	assert.Equal("websocket-stream", updatedStream.Name)
	assert.NoError(err)
}

func TestWebSocketClientClosedOnSend(t *testing.T) {

	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			BatchSize:     5,
			Type:          "websocket",
			WebSocket: &webSocketActionInfo{
				Topic: "test1",
			},
		}, db, 200)
	defer svr.Close()
	defer close(eventStream)
	defer stream.stop()

	mws := stream.wsChannels.(*mockWebSocket)
	wsa := stream.action.(*webSocketAction)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		wsa.attemptBatch(0, 0, []*eventData{})
		wg.Done()
	}()

	close(mws.closing)
	wg.Wait()

}

func TestWebSocketClientClosedOnReceive(t *testing.T) {

	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			BatchSize:     5,
			Type:          "websocket",
			WebSocket: &webSocketActionInfo{
				Topic: "test1",
			},
		}, db, 200)
	defer svr.Close()
	defer close(eventStream)
	defer stream.stop()

	mws := stream.wsChannels.(*mockWebSocket)
	wsa := stream.action.(*webSocketAction)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		wsa.attemptBatch(0, 0, []*eventData{})
		wg.Done()
	}()

	<-mws.sender

	close(mws.closing)
	wg.Wait()

}

func TestUpdateStreamMissingWebhookURL(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookActionInfo{},
		}, db, 200)
	defer svr.Close()

	s := setupTestSubscription(assert, sm, stream, "mySubName")
	assert.Equal("mySubName", s.Name)

	// We expect three events to be sent to the webhook
	// With the default batch size of 1, that means three separate requests
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		<-eventStream
		<-eventStream
		<-eventStream
		wg.Done()
	}()
	wg.Wait()

	ctx := context.Background()
	updateSpec := &StreamInfo{
		Webhook: &webhookActionInfo{
			URL:               "",
			TLSkipHostVerify:  true,
			RequestTimeoutSec: 5,
		},
	}
	_, err := sm.UpdateStream(ctx, stream.spec.ID, updateSpec)
	assert.EqualError(err, errors.EventStreamsWebhookNoURL)
	err = sm.DeleteSubscription(ctx, s.ID)
	assert.NoError(err)
	err = sm.DeleteStream(ctx, stream.spec.ID)
	assert.NoError(err)
	sm.Close()
}

func TestUpdateStreamInvalidWebhookURL(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookActionInfo{},
		}, db, 200)
	defer svr.Close()

	s := setupTestSubscription(assert, sm, stream, "mySubName")
	assert.Equal("mySubName", s.Name)

	// We expect three events to be sent to the webhook
	// With the default batch size of 1, that means three separate requests
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		<-eventStream
		<-eventStream
		<-eventStream
		wg.Done()
	}()
	wg.Wait()

	ctx := context.Background()
	updateSpec := &StreamInfo{
		Webhook: &webhookActionInfo{
			URL:               ":badurl",
			TLSkipHostVerify:  true,
			RequestTimeoutSec: 5,
		},
	}
	_, err := sm.UpdateStream(ctx, stream.spec.ID, updateSpec)
	assert.EqualError(err, errors.EventStreamsWebhookInvalidURL)
	err = sm.DeleteSubscription(ctx, s.ID)
	assert.NoError(err)
	err = sm.DeleteStream(ctx, stream.spec.ID)
	assert.NoError(err)
	sm.Close()
}

func TestUpdateStreamDuplicateCall(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	db, _ := kvstore.NewLDBKeyValueStore(dir)
	_, stream, svr, _ := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookActionInfo{},
		}, db, 200)
	defer svr.Close()

	err := stream.preUpdateStream()
	assert.NoError(err)

	err = stream.preUpdateStream()
	assert.Regexp("Update to event stream already in progress", err)
}
