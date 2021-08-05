// Copyright 2020 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ws

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	ws "github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"

	"github.com/stretchr/testify/assert"
)

func newTestWebSocketServer() (*webSocketServer, *httptest.Server) {
	s := NewWebSocketServer().(*webSocketServer)
	r := &httprouter.Router{}
	s.AddRoutes(r)
	ts := httptest.NewServer(r)
	return s, ts
}

func TestConnectSendReceiveCycle(t *testing.T) {
	assert := assert.New(t)

	w, ts := newTestWebSocketServer()
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	c, _, err := ws.DefaultDialer.Dial(u.String(), nil)
	assert.NoError(err)

	c.WriteJSON(&webSocketCommandMessage{
		Type: "listen",
	})

	s, _, r, _ := w.GetChannels("")

	s <- "Hello World"

	var val string
	c.ReadJSON(&val)
	assert.Equal("Hello World", val)

	c.WriteJSON(&webSocketCommandMessage{
		Type: "ignoreme",
	})

	c.WriteJSON(&webSocketCommandMessage{
		Type: "ack",
	})
	err = <-r
	assert.NoError(err)

	s <- "Don't Panic!"

	c.ReadJSON(&val)
	assert.Equal("Don't Panic!", val)

	c.WriteJSON(&webSocketCommandMessage{
		Type:    "error",
		Message: "Panic!",
	})

	err = <-r
	assert.EqualError(err, "Error received from WebSocket client: Panic!")

	w.Close()

}

func TestConnectTopicIsolation(t *testing.T) {
	assert := assert.New(t)

	w, ts := newTestWebSocketServer()
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	c1, _, err := ws.DefaultDialer.Dial(u.String(), nil)
	assert.NoError(err)
	c2, _, err := ws.DefaultDialer.Dial(u.String(), nil)
	assert.NoError(err)

	c1.WriteJSON(&webSocketCommandMessage{
		Type:  "listen",
		Topic: "topic1",
	})

	c2.WriteJSON(&webSocketCommandMessage{
		Type:  "listen",
		Topic: "topic2",
	})

	s1, _, r1, _ := w.GetChannels("topic1")
	s2, _, r2, _ := w.GetChannels("topic2")

	s1 <- "Hello Number 1"
	s2 <- "Hello Number 2"

	var val string
	c1.ReadJSON(&val)
	assert.Equal("Hello Number 1", val)
	c1.WriteJSON(&webSocketCommandMessage{
		Type:  "ack",
		Topic: "topic1",
	})
	err = <-r1
	assert.NoError(err)

	c2.ReadJSON(&val)
	assert.Equal("Hello Number 2", val)
	c2.WriteJSON(&webSocketCommandMessage{
		Type:  "ack",
		Topic: "topic2",
	})
	err = <-r2
	assert.NoError(err)

	w.Close()

}

func TestConnectAbandonRequest(t *testing.T) {
	assert := assert.New(t)

	w, ts := newTestWebSocketServer()
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	c, _, err := ws.DefaultDialer.Dial(u.String(), nil)
	assert.NoError(err)

	c.WriteJSON(&webSocketCommandMessage{
		Type: "listen",
	})
	_, _, r, closing := w.GetChannels("")

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		select {
		case <-r:
			break
		case <-closing:
			break
		}
		wg.Done()
	}()

	// Close the client while we've got an active read stream
	c.Close()

	// We whould find the read stream closes out
	wg.Wait()
	w.Close()

}

func TestSpuriousAckProcessing(t *testing.T) {
	assert := assert.New(t)

	w, ts := newTestWebSocketServer()
	defer ts.Close()
	w.processingTimeout = 1 * time.Millisecond

	u, err := url.Parse(ts.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	c, _, err := ws.DefaultDialer.Dial(u.String(), nil)
	assert.NoError(err)

	c.WriteJSON(&webSocketCommandMessage{
		Type:  "ack",
		Topic: "mytopic",
	})
	c.WriteJSON(&webSocketCommandMessage{
		Type:  "ack",
		Topic: "mytopic",
	})
	c.Close()

	for len(w.connections) > 0 {
		time.Sleep(1 * time.Millisecond)
	}
	w.Close()
}

func TestConnectBadWebsocketHandshake(t *testing.T) {
	assert := assert.New(t)

	w, ts := newTestWebSocketServer()
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	u.Path = "/ws"

	res, err := http.Get(u.String())
	assert.NoError(err)
	assert.Equal(400, res.StatusCode)

	w.Close()

}

func TestBroadcast(t *testing.T) {
	assert := assert.New(t)

	w, ts := newTestWebSocketServer()
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	topic := "banana"
	c, _, err := ws.DefaultDialer.Dial(u.String(), nil)
	assert.NoError(err)

	c.WriteJSON(&webSocketCommandMessage{
		Type:  "listen",
		Topic: topic,
	})

	// Wait until the client has subscribed to the topic before proceeding
	for len(w.topicMap[topic]) == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	_, b, _, _ := w.GetChannels(topic)
	b <- "Hello World"

	var val string
	c.ReadJSON(&val)
	assert.Equal("Hello World", val)

	b <- "Hello World Again"

	c.ReadJSON(&val)
	assert.Equal("Hello World Again", val)

	w.Close()
}

func TestBroadcastDefaultTopic(t *testing.T) {
	assert := assert.New(t)

	w, ts := newTestWebSocketServer()
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	topic := ""
	c, _, err := ws.DefaultDialer.Dial(u.String(), nil)
	assert.NoError(err)

	c.WriteJSON(&webSocketCommandMessage{
		Type: "listen",
	})

	// Wait until the client has subscribed to the topic before proceeding
	for len(w.topicMap[topic]) == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	_, b, _, _ := w.GetChannels(topic)
	b <- "Hello World"

	var val string
	c.ReadJSON(&val)
	assert.Equal("Hello World", val)

	b <- "Hello World Again"

	c.ReadJSON(&val)
	assert.Equal("Hello World Again", val)

	w.Close()
}

func TestRecvNotOk(t *testing.T) {
	assert := assert.New(t)

	w, ts := newTestWebSocketServer()
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	topic := ""
	c, _, err := ws.DefaultDialer.Dial(u.String(), nil)
	assert.NoError(err)

	c.WriteJSON(&webSocketCommandMessage{
		Type: "listen",
	})

	// Wait until the client has subscribed to the topic before proceeding
	for len(w.topicMap[topic]) == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	_, b, _, _ := w.GetChannels(topic)
	close(b)
	w.Close()
}

func TestSendReply(t *testing.T) {
	assert := assert.New(t)

	w, ts := newTestWebSocketServer()
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	c, _, err := ws.DefaultDialer.Dial(u.String(), nil)
	assert.NoError(err)

	c.WriteJSON(&webSocketCommandMessage{
		Type: "listenReplies",
	})

	// Wait until the client has subscribed to the topic before proceeding
	for len(w.replyMap) == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	w.SendReply("Hello World")

	var val string
	c.ReadJSON(&val)
	assert.Equal("Hello World", val)
}
