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

package kldevents

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

	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldeth"
	"github.com/kaleido-io/ethconnect/internal/kldkvstore"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestConstructorNoSpec(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(newTestSubscriptionManager(), nil)
	assert.EqualError(err, "No ID")
}

func TestConstructorBadType(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(newTestSubscriptionManager(), &StreamInfo{
		ID:   "123",
		Type: "random",
	})
	assert.EqualError(err, "Unknown action type 'random'")
}

func TestConstructorMissingWebhook(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(newTestSubscriptionManager(), &StreamInfo{
		ID:   "123",
		Type: "webhook",
	})
	assert.EqualError(err, "Must specify webhook.url for action type 'webhook'")
}

func TestConstructorBadWebhookURL(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(newTestSubscriptionManager(), &StreamInfo{
		ID:   "123",
		Type: "webhook",
		Webhook: &webhookAction{
			URL: ":badurl",
		},
	})
	assert.EqualError(err, "Invalid URL in webhook action")
}

func testEvent(subID string) *eventData {
	return &eventData{
		SubID:         subID,
		batchComplete: func(*eventData) {},
	}
}

func newTestStreamForBatching(spec *StreamInfo, status ...int) (*subscriptionMGR, *eventStream, *httptest.Server, chan []*eventData) {
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
	spec.Type = "WEBHOOK"
	spec.Webhook.URL = svr.URL
	spec.Webhook.Headers = map[string]string{"x-my-header": "my-value"}
	sm := newTestSubscriptionManager()
	sm.config().WebhooksAllowPrivateIPs = true
	sm.config().EventPollingIntervalSec = 0
	ctx := context.Background()
	stream, _ := sm.AddStream(ctx, spec)
	return sm, sm.streams[stream.ID], svr, eventStream
}

func TestBatchTimeout(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize:      10,
			BatchTimeoutMS: 50,
			Webhook:        &webhookAction{},
		}, 200)
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
			Webhook:        &webhookAction{},
		}, 200)
	defer close(eventStream)
	defer svr.Close()

	stream.handleEvent(testEvent(fmt.Sprintf("sub1")))
	time.Sleep(10 * time.Millisecond)
	stream.stop()
	time.Sleep(10 * time.Millisecond)
	assert.True(stream.processorDone)
	assert.True(stream.dispatcherDone)
}

func TestBatchSizeCap(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize: 10000000,
			Webhook:   &webhookAction{},
		}, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	assert.Equal(uint64(MaxBatchSize), stream.spec.BatchSize)
}

func TestBlockingBehavior(t *testing.T) {
	assert := assert.New(t)
	_, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize:            1,
			Webhook:              &webhookAction{},
			ErrorHandling:        ErrorHandlingBlock,
			BlockedRetryDelaySec: 1,
		}, 404)
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
			Webhook:              &webhookAction{},
			ErrorHandling:        ErrorHandlingSkip,
			BlockedRetryDelaySec: 1,
		}, 404 /* fail the requests */)
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
			Webhook:              &webhookAction{},
			ErrorHandling:        ErrorHandlingBlock,
			RetryTimeoutSec:      1,
			BlockedRetryDelaySec: 1,
		}, 404, 500, 503, 504, 200)
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
			Webhook:       &webhookAction{},
		}, 200)
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
			Webhook:       &webhookAction{},
		}, 200)
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
			Webhook:       &webhookAction{},
		}, 200)
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

func setupTestSubscription(assert *assert.Assertions, sm *subscriptionMGR, stream *eventStream) *SubscriptionInfo {
	log.SetLevel(log.DebugLevel)
	testDataBytes, err := ioutil.ReadFile("../../test/simplevents_logs.json")
	assert.NoError(err)
	var testData []*logEntry
	json.Unmarshal(testDataBytes, &testData)

	callCount := 0
	rpc := kldeth.NewMockRPCClientForSync(nil, func(method string, res interface{}, args ...interface{}) {
		callCount++
		if method == "eth_blockNumber" || method == "eth_newFilter" {
		} else if method == "eth_getFilterLogs" {
			*(res.(*[]*logEntry)) = testData[0:2]
		} else if method == "eth_getFilterChanges" && (callCount == 4) {
			*(res.(*[]*logEntry)) = testData[2:]
		} else if method == "eth_getFilterChanges" {
			*(res.(*[]*logEntry)) = []*logEntry{}
		}
	})
	sm.rpc = rpc

	event := &kldbind.ABIEvent{
		Name: "Changed",
		Inputs: []kldbind.ABIArgument{
			kldbind.ABIArgument{
				Name:    "from",
				Type:    kldbind.ABITypeKnown("address"),
				Indexed: true,
			},
			kldbind.ABIArgument{
				Name:    "i",
				Type:    kldbind.ABITypeKnown("int64"),
				Indexed: true,
			},
			kldbind.ABIArgument{
				Name:    "s",
				Type:    kldbind.ABITypeKnown("string"),
				Indexed: true,
			},
			kldbind.ABIArgument{
				Name: "h",
				Type: kldbind.ABITypeKnown("bytes32"),
			},
			kldbind.ABIArgument{
				Name: "m",
				Type: kldbind.ABITypeKnown("string"),
			},
		},
	}
	addr := kldbind.HexToAddress("0x167f57a13a9c35ff92f0649d2be0e52b4f8ac3ca")
	ctx := context.Background()
	s, _ := sm.AddSubscription(ctx, &addr, event, stream.spec.ID, "")
	return s
}

func TestProcessEventsEnd2End(t *testing.T) {
	assert := assert.New(t)
	dir := tempdir(t)
	defer cleanup(t, dir)

	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			BatchSize: 1,
			Webhook:   &webhookAction{},
		}, 200)
	sm.db, _ = kldkvstore.NewLDBKeyValueStore(dir)
	defer svr.Close()

	s := setupTestSubscription(assert, sm, stream)

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
		assert.Equal("150676", e2s[0].BlockNumber)
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

func TestCheckpointRecovery(t *testing.T) {
	assert := assert.New(t)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookAction{},
		}, 200)
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

	s := setupTestSubscription(assert, sm, stream)

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
	sub.rpc = kldeth.NewMockRPCClientForSync(nil, func(method string, res interface{}, args ...interface{}) {
		if method == "eth_newFilter" {
			newFilterBlock = args[0].(*ethFilter).FromBlock.ToInt().Uint64()
			t.Logf("New filter block after checkpoint recovery: %d", newFilterBlock)
		} else {
			*(res.(*[]*logEntry)) = []*logEntry{}
		}
	})

	stream.resume()
	for stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}
	wg.Wait()
	for uint64(150722) != newFilterBlock {
		time.Sleep(1 * time.Millisecond)
	}

}

func TestWithoutCheckpointRecovery(t *testing.T) {
	assert := assert.New(t)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookAction{},
		}, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	stream.suspend()
	for !stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}

	s := setupTestSubscription(assert, sm, stream)

	var initialEndBlock string
	sub := sm.subscriptions[s.ID]
	sub.filterStale = true
	sub.rpc = kldeth.NewMockRPCClientForSync(nil, func(method string, res interface{}, args ...interface{}) {
		if method == "eth_blockNumber" {
		} else if method == "eth_newFilter" {
			initialEndBlock = args[0].(*ethFilter).ToBlock
			t.Logf("New filter block after recovery with no checkpoint: %s", initialEndBlock)
		} else {
			*(res.(*[]*logEntry)) = []*logEntry{}
		}
	})

	stream.resume()
	for stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}
	for initialEndBlock != "latest" {
		time.Sleep(1 * time.Millisecond)
	}

}

func TestMarkStaleOnError(t *testing.T) {
	assert := assert.New(t)
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookAction{},
		}, 200)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	stream.suspend()
	for !stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}

	s := setupTestSubscription(assert, sm, stream)
	sm.subscriptions[s.ID].filterStale = false

	sub := sm.subscriptions[s.ID]
	sub.rpc = kldeth.NewMockRPCClientForSync(fmt.Errorf("filter not found"), nil)

	stream.resume()
	for stream.pollerDone {
		time.Sleep(1 * time.Millisecond)
	}
	for !sub.filterStale {
		time.Sleep(1 * time.Millisecond)
	}

}

func TestStoreCheckpointLoadError(t *testing.T) {
	sm, stream, svr, eventStream := newTestStreamForBatching(
		&StreamInfo{
			ErrorHandling: ErrorHandlingBlock,
			Webhook:       &webhookAction{},
		}, 200)
	mockKV := kldkvstore.NewMockKV(fmt.Errorf("pop"))
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
			Webhook:       &webhookAction{},
		}, 200)
	mockKV := kldkvstore.NewMockKV(nil)
	mockKV.StoreErr = fmt.Errorf("pop")
	sm.db = mockKV
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
	setupTestSubscription(assert, sm, stream)
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
			Webhook:       &webhookAction{},
		}, 200)
	mockKV := kldkvstore.NewMockKV(nil)
	mockKV.StoreErr = fmt.Errorf("pop")
	sm.db = mockKV
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	stream.processBatch(0, []*eventData{})
}
