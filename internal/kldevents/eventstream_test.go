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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConstructorNoSpec(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(true, nil)
	assert.EqualError(err, "No action specified")
}

func TestConstructorBadType(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(true, &StreamInfo{
		Type: "random",
	})
	assert.EqualError(err, "Unknown action type 'random'")
}

func TestConstructorMissingWebhook(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(true, &StreamInfo{
		Type: "webhook",
	})
	assert.EqualError(err, "Must specify webhook.url for action type 'webhook'")
}

func TestConstructorBadWebhookURL(t *testing.T) {
	assert := assert.New(t)
	_, err := newEventStream(true, &StreamInfo{
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

func newTestStreamForBatching(spec *StreamInfo, status ...int) (*eventStream, *httptest.Server, chan []*eventData) {
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
	stream, _ := newEventStream(true, spec)
	return stream, svr, eventStream
}

func TestBatchTimeout(t *testing.T) {
	assert := assert.New(t)
	stream, svr, eventStream := newTestStreamForBatching(&StreamInfo{
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

}

func TestStopDuringTimeout(t *testing.T) {
	assert := assert.New(t)
	stream, svr, eventStream := newTestStreamForBatching(&StreamInfo{
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
	stream, svr, eventStream := newTestStreamForBatching(&StreamInfo{
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
	stream, svr, eventStream := newTestStreamForBatching(&StreamInfo{
		BatchSize:            10,
		Webhook:              &webhookAction{},
		ErrorHandling:        ErrorHandlingBlock,
		BlockedRetryDelaySec: 1,
	}, 404)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	complete := false
	thrown := false
	go func() { <-eventStream; thrown = true }()
	stream.handleEvent(&eventData{
		SubID:         "sub1",
		batchComplete: func(*eventData) { complete = true },
	})
	time.Sleep(10 * time.Millisecond)
	assert.True(thrown)
	assert.False(complete)
}

func TestSkippingBehavior(t *testing.T) {
	assert := assert.New(t)
	stream, svr, eventStream := newTestStreamForBatching(&StreamInfo{
		BatchSize:            10,
		Webhook:              &webhookAction{},
		ErrorHandling:        ErrorHandlingSkip,
		BlockedRetryDelaySec: 1,
	}, 404)
	defer close(eventStream)
	defer svr.Close()
	defer stream.stop()

	complete := false
	thrown := false
	go func() { <-eventStream; thrown = true }()
	stream.handleEvent(&eventData{
		SubID:         "sub1",
		batchComplete: func(*eventData) { complete = true },
	})
	time.Sleep(100 * time.Millisecond)
	assert.True(thrown)
	assert.True(complete)
}

func TestBackoffRetry(t *testing.T) {
	assert := assert.New(t)
	stream, svr, eventStream := newTestStreamForBatching(&StreamInfo{
		BatchSize:            10,
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
	stream, svr, eventStream := newTestStreamForBatching(&StreamInfo{
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
	stream, svr, eventStream := newTestStreamForBatching(&StreamInfo{
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
	stream, svr, eventStream := newTestStreamForBatching(&StreamInfo{
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
	assert.Equal(uint64(11), stream.inFlight)

	for i := 0; i < 11; i++ {
		<-eventStream
	}
	for stream.isBlocked() {
		time.Sleep(1 * time.Millisecond)
	}
	assert.Equal(uint64(0), stream.inFlight)

}
