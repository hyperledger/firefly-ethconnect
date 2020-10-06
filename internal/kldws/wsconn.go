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

package kldws

import (
	"reflect"
	"sync"
	"time"

	ws "github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"

	"github.com/kaleido-io/ethconnect/internal/klderrors"
	"github.com/kaleido-io/ethconnect/internal/kldutils"
)

type webSocketConnection struct {
	id      string
	server  *webSocketServer
	conn    *ws.Conn
	mux     sync.Mutex
	closed  bool
	topics  chan *webSocketTopic
	receive chan error
}

type webSocketCommandMessage struct {
	Type    string `json:"type,omitempty"`
	Topic   string `json:"topic,omitempty"`
	Message string `json:"message,omitempty"`
}

func newConnection(server *webSocketServer, conn *ws.Conn) *webSocketConnection {
	wsc := &webSocketConnection{
		id:      kldutils.UUIDv4(),
		server:  server,
		conn:    conn,
		topics:  make(chan *webSocketTopic),
		receive: make(chan error),
	}
	go wsc.listen()
	go wsc.sender()
	return wsc
}

func (c *webSocketConnection) close() {
	c.mux.Lock()
	defer c.mux.Unlock()
	if !c.closed {
		c.closed = true
		c.conn.Close()
		close(c.receive)
		close(c.topics)
		c.server.connectionClosed(c)
		log.Infof("WS/%s: Disconnected", c.id)
	}
}

func (c *webSocketConnection) sender() {
	defer c.close()
	topics := make(map[string]*webSocketTopic)
	buildCases := func() []reflect.SelectCase {
		cases := make([]reflect.SelectCase, len(topics)+1)
		i := 0
		for _, t := range topics {
			cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(t.senderChannel)}
			i++
		}
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(c.topics)}
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
			t := value.Interface().(*webSocketTopic)
			topics[t.topic] = t
			cases = buildCases()
		} else {
			// Message from one of the existing topics
			c.conn.WriteJSON(value.Interface())
		}
	}
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
		switch msg.Type {
		case "listen":
			c.topics <- t
		case "ack":
			c.handleAckOrError(t, nil)
		case "error":
			c.handleAckOrError(t, klderrors.Errorf(klderrors.EventStreamsWebSocketErrorFromClient, msg.Message))
		default:
			log.Errorf("WS/%s: Unexpected message type: %+v", c.id, msg)
		}
	}
}

func (c *webSocketConnection) handleAckOrError(t *webSocketTopic, err error) {
	isError := err != nil
	select {
	case <-time.After(c.server.processingTimeout):
		log.Errorf("WS/%s: response (error='%t') on topic '%s'. We were not available to process it after %.2f seconds. Closing connection", c.id, isError, t.topic, c.server.processingTimeout.Seconds())
		c.close()
	case t.receiverChannel <- err:
		log.Debugf("WS/%s: response (error='%t') on topic '%s' passed on for processing", c.id, isError, t.topic)
		break
	}
}
