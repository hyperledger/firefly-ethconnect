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
	"reflect"
	"strings"
	"sync"

	ws "github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"

	"github.com/kaleido-io/ethconnect/internal/errors"
	"github.com/kaleido-io/ethconnect/internal/utils"
)

type webSocketConnection struct {
	id        string
	server    *webSocketServer
	conn      *ws.Conn
	mux       sync.Mutex
	closed    bool
	topics    map[string]*webSocketTopic
	broadcast chan interface{}
	newTopic  chan bool
	receive   chan error
	closing   chan struct{}
}

type webSocketCommandMessage struct {
	Type    string `json:"type,omitempty"`
	Topic   string `json:"topic,omitempty"`
	Message string `json:"message,omitempty"`
}

func newConnection(server *webSocketServer, conn *ws.Conn) *webSocketConnection {
	wsc := &webSocketConnection{
		id:        utils.UUIDv4(),
		server:    server,
		conn:      conn,
		newTopic:  make(chan bool),
		topics:    make(map[string]*webSocketTopic),
		broadcast: make(chan interface{}),
		receive:   make(chan error),
		closing:   make(chan struct{}),
	}
	go wsc.listen()
	go wsc.sender()
	return wsc
}

func (c *webSocketConnection) close() {
	c.mux.Lock()
	if !c.closed {
		c.closed = true
		c.conn.Close()
		close(c.closing)
	}
	c.mux.Unlock()

	for _, t := range c.topics {
		c.server.cycleTopic(t)
	}
	c.server.connectionClosed(c)
	log.Infof("WS/%s: Disconnected", c.id)
}

func (c *webSocketConnection) sender() {
	defer c.close()
	buildCases := func() []reflect.SelectCase {
		c.mux.Lock()
		defer c.mux.Unlock()
		cases := make([]reflect.SelectCase, len(c.topics)+3)
		i := 0
		for _, t := range c.topics {
			cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(t.senderChannel)}
			i++
		}
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(c.broadcast)}
		i++
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(c.closing)}
		i++
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(c.newTopic)}
		return cases
	}
	cases := buildCases()
	for {
		chosen, value, ok := reflect.Select(cases)
		if !ok {
			log.Infof("WS/%s: Closing", c.id)
			return
		}

		if chosen == len(cases)-1 {
			// Addition of a new topic
			cases = buildCases()
		} else {
			// Message from one of the existing topics
			c.conn.WriteJSON(value.Interface())
		}
	}
}

func (c *webSocketConnection) listenTopic(t *webSocketTopic) {
	c.mux.Lock()
	c.topics[t.topic] = t
	c.server.ListenOnTopic(c, t.topic)
	c.mux.Unlock()
	select {
	case c.newTopic <- true:
	case <-c.closing:
	}
}

func (c *webSocketConnection) listenReplies() {
	c.server.ListenForReplies(c)
}

func (c *webSocketConnection) listen() {
	defer c.close()
	log.Infof("WS/%s: Connected", c.id)
	for {
		var msg webSocketCommandMessage
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			log.Errorf("WS/%s: Error: %s", c.id, err)
			return
		}
		log.Debugf("WS/%s: Received: %+v", c.id, msg)

		t := c.server.getTopic(msg.Topic)
		switch strings.ToLower(msg.Type) {
		case "listen":
			c.listenTopic(t)
		case "listenreplies":
			c.listenReplies()
		case "ack":
			c.handleAckOrError(t, nil)
		case "error":
			c.handleAckOrError(t, errors.Errorf(errors.EventStreamsWebSocketErrorFromClient, msg.Message))
		default:
			log.Errorf("WS/%s: Unexpected message type: %+v", c.id, msg)
		}
	}
}

func (c *webSocketConnection) handleAckOrError(t *webSocketTopic, err error) {
	isError := err != nil
	select {
	case t.receiverChannel <- err:
		log.Debugf("WS/%s: response (error='%t') on topic '%s' passed on for processing", c.id, isError, t.topic)
		break
	default:
		log.Debugf("WS/%s: spurious ack received (error='%t') on topic '%s'", c.id, isError, t.topic)
		break
	}
}
