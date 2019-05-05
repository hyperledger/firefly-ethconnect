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
	"io/ioutil"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/kaleido-io/ethconnect/internal/kldbind"
	"github.com/kaleido-io/ethconnect/internal/kldeth"

	"github.com/stretchr/testify/assert"
)

type mockSubMgr struct {
	action       *action
	subscription *subscription
	err          error
}

func (m *mockSubMgr) actionByID(string) (*action, error) {
	return m.action, m.err
}

func (m *mockSubMgr) subscriptionByID(string) (*subscription, error) {
	return m.subscription, m.err
}

func newTestAction() *action {
	a, _ := newAction("action1", true, &actionSpec{
		Type: "WebHook",
		Webhook: &webhookAction{
			URL: "http://hello.example.com/world",
		},
	}, 0)
	return a
}

func TestCreateWebhookSub(t *testing.T) {
	assert := assert.New(t)

	rpc := kldeth.NewMockRPCClientForSync(nil, nil)
	kv := newMockKV()
	event := &kldbind.ABIEvent{
		Name: "glastonbury",
		Inputs: []kldbind.ABIArgument{
			kldbind.ABIArgument{
				Name: "field",
				Type: kldbind.ABITypeKnown("address"),
			},
			kldbind.ABIArgument{
				Name: "tents",
				Type: kldbind.ABITypeKnown("uint256"),
			},
			kldbind.ABIArgument{
				Name: "mud",
				Type: kldbind.ABITypeKnown("bool"),
			},
		},
	}
	m := &mockSubMgr{
		action: newTestAction(),
	}

	s, err := newSubscription(m, kv, rpc, nil, event, "actionID")
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)

	s1, err := restoreSubscription(m, kv, rpc, s.info.ID, &big.Int{})
	assert.NoError(err)

	assert.Equal(s.info.ID, s1.info.ID)
	assert.Equal("*:glastonbury(address,uint256,bool)", s1.info.Name)
	assert.Equal(event.Id(), s.info.Filter.Topics[0][0])
}

func TestCreateWebhookSubWithAddr(t *testing.T) {
	assert := assert.New(t)

	rpc := kldeth.NewMockRPCClientForSync(nil, nil)
	kv := newMockKV()
	m := &mockSubMgr{action: newTestAction()}
	event := &kldbind.ABIEvent{
		Name:      "devcon",
		Anonymous: true,
	}

	addr := kldbind.HexToAddress("0x0123456789abcDEF0123456789abCDef01234567")
	s, err := newSubscription(m, kv, rpc, &addr, event, "actionid")
	assert.NoError(err)
	assert.NotEmpty(s.info.ID)
	assert.Equal(event.Id(), s.info.Filter.Topics[0][0])
	assert.Equal("0x0123456789abcDEF0123456789abCDef01234567:devcon()", s.info.Name)
}

func TestRestoreSubscriptionMissing(t *testing.T) {
	assert := assert.New(t)
	kv := newMockKV()
	kv.err = fmt.Errorf("pop")
	m := &mockSubMgr{action: newTestAction()}
	_, err := restoreSubscription(m, kv, nil, "missing", &big.Int{})
	assert.EqualError(err, "Failed to read subscription from key value store: pop")
}

func TestRestoreSubscriptionBad(t *testing.T) {
	assert := assert.New(t)
	kv := newMockKV()
	m := &mockSubMgr{action: newTestAction()}
	_, err := restoreSubscription(m, kv, nil, "bad data", &big.Int{})
	assert.EqualError(err, "Failed to restore subscription from key value store: unexpected end of JSON input")
}

func TestCreateSubscriptionNoEvent(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{}
	m := &mockSubMgr{action: newTestAction()}
	_, err := newSubscription(m, nil, nil, nil, event, "actionid")
	assert.EqualError(err, "Solidity event name must be specified")
}

func TestCreateSubscriptionPersistFailure(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{Name: "party"}
	rpc := kldeth.NewMockRPCClientForSync(nil, nil)
	kv := newMockKV()
	kv.err = fmt.Errorf("pop")
	m := &mockSubMgr{action: newTestAction()}
	_, err := newSubscription(m, kv, rpc, nil, event, "actionid")
	assert.EqualError(err, "Failed to store subscription info: pop")
}

func TestCreateSubscriptionMissingAction(t *testing.T) {
	assert := assert.New(t)
	event := &kldbind.ABIEvent{Name: "party"}
	m := &mockSubMgr{err: fmt.Errorf("nope")}
	_, err := newSubscription(m, nil, nil, nil, event, "actionid")
	assert.EqualError(err, "nope")
}

func TestProcessEventsEnd2End(t *testing.T) {
	assert := assert.New(t)

	mux := http.NewServeMux()
	eventStream := make(chan []*eventData)
	defer close(eventStream)
	mux.HandleFunc("/", func(res http.ResponseWriter, req *http.Request) {
		var events []*eventData
		err := json.NewDecoder(req.Body).Decode(&events)
		assert.NoError(err)
		eventStream <- events
		res.WriteHeader(200)
	})
	svr := httptest.NewServer(mux)
	defer svr.Close()
	action, _ := newAction("action1", true, &actionSpec{
		Type: "WebHook",
		Webhook: &webhookAction{
			URL: svr.URL,
		},
	}, 0)
	m := &mockSubMgr{action: action}

	testDataBytes, err := ioutil.ReadFile("../../test/simplevents_logs.json")
	assert.NoError(err)
	var testData []*logEntry
	json.Unmarshal(testDataBytes, &testData)

	callCount := 0
	rpc := kldeth.NewMockRPCClientForSync(nil, func(method string, res interface{}) {
		t.Logf("CallContext %d: %s", callCount, method)
		callCount++
		if method == "eth_newFilter" {
			assert.True(callCount < 2)
		} else if method == "eth_getFilterLogs" {
			assert.Equal(2, callCount)
			*(res.(*[]*logEntry)) = testData[0:2]
		} else if method == "eth_getFilterChanges" {
			assert.Equal(3, callCount)
			*(res.(*[]*logEntry)) = testData[2:]
		}
	})

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
	kv := newMockKV()
	addr := kldbind.HexToAddress("0x167f57a13a9c35ff92f0649d2be0e52b4f8ac3ca")
	s, err := newSubscription(m, kv, rpc, &addr, event, "actionid")
	assert.NoError(err)

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

	err = s.processNewEvents()
	assert.NoError(err)
	err = s.processNewEvents()
	assert.NoError(err)
	wg.Wait()

}
