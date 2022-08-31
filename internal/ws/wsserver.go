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
	"reflect"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
)

// WebSocketChannels is provided to allow us to do a blocking send to a namespace that will complete once a client connects on it
// We also provide a channel to listen on for closing of the connection, to allow a select to wake on a blocking send
type WebSocketChannels interface {
	GetChannels(topic string) (chan<- interface{}, chan<- interface{}, <-chan error)
	SendReply(message interface{})
}

// WebSocketServer is the full server interface with the init call
type WebSocketServer interface {
	WebSocketChannels
	AddRoutes(r *httprouter.Router)
	Close()
}

type webSocketServer struct {
	processingTimeout time.Duration
	mux               sync.Mutex
	topics            map[string]*webSocketTopic
	topicMap          map[string]map[string]*webSocketConnection
	replyMap          map[string]*webSocketConnection
	newTopic          chan bool
	replyChannel      chan interface{}
	upgrader          *websocket.Upgrader
	connections       map[string]*webSocketConnection
}

type webSocketTopic struct {
	topic            string
	senderChannel    chan interface{}
	broadcastChannel chan interface{}
	receiverChannel  chan error
}

// NewWebSocketServer create a new server with a simplified interface
func NewWebSocketServer() WebSocketServer {
	s := &webSocketServer{
		connections:       make(map[string]*webSocketConnection),
		topics:            make(map[string]*webSocketTopic),
		topicMap:          make(map[string]map[string]*webSocketConnection),
		replyMap:          make(map[string]*webSocketConnection),
		newTopic:          make(chan bool),
		replyChannel:      make(chan interface{}),
		processingTimeout: 30 * time.Second,
		upgrader: &websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
	go s.processBroadcasts()
	go s.processReplies()
	return s
}

func (s *webSocketServer) handler(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("WebSocket upgrade failed: %s", err)
		return
	}
	s.mux.Lock()
	defer s.mux.Unlock()
	c := newConnection(s, conn)
	s.connections[c.id] = c
}

func (s *webSocketServer) cycleTopic(connInfo string, t *webSocketTopic) {
	s.mux.Lock()
	defer s.mux.Unlock()

	// When a connection that was listening on a topic closes, we need to wake anyone
	// that was listening for a response
	select {
	case t.receiverChannel <- errors.Errorf(errors.WebSocketClosed, connInfo):
	default:
	}
}

func (s *webSocketServer) connectionClosed(c *webSocketConnection) {
	s.mux.Lock()
	defer s.mux.Unlock()
	delete(s.connections, c.id)
	delete(s.replyMap, c.id)
	for _, topic := range c.topics {
		delete(s.topicMap[topic.topic], c.id)
	}
}

func (s *webSocketServer) AddRoutes(r *httprouter.Router) {
	r.GET("/ws", s.handler)
}

func (s *webSocketServer) Close() {
	for _, c := range s.connections {
		c.close()
	}
}

func (s *webSocketServer) getTopic(topic string) *webSocketTopic {
	s.mux.Lock()
	t, exists := s.topics[topic]
	if !exists {
		t = &webSocketTopic{
			topic:            topic,
			senderChannel:    make(chan interface{}),
			broadcastChannel: make(chan interface{}),
			receiverChannel:  make(chan error, 1),
		}
		s.topics[topic] = t
		s.topicMap[topic] = make(map[string]*webSocketConnection)
	}
	s.mux.Unlock()
	if !exists {
		// Signal to the broadcaster that a new topic has been added
		s.newTopic <- true
	}
	return t
}

func (s *webSocketServer) GetChannels(topic string) (chan<- interface{}, chan<- interface{}, <-chan error) {
	t := s.getTopic(topic)
	return t.senderChannel, t.broadcastChannel, t.receiverChannel
}

func (s *webSocketServer) ListenOnTopic(c *webSocketConnection, topic string) {
	// Track that this connection is interested in this topic
	s.topicMap[topic][c.id] = c
}

func (s *webSocketServer) ListenForReplies(c *webSocketConnection) {
	s.replyMap[c.id] = c
}

func (s *webSocketServer) SendReply(message interface{}) {
	s.replyChannel <- message
}

func (s *webSocketServer) processBroadcasts() {
	var topics []string
	buildCases := func() []reflect.SelectCase {
		// only hold the lock while we're building the list of cases (not while doing the select)
		s.mux.Lock()
		defer s.mux.Unlock()
		topics = make([]string, len(s.topics))
		cases := make([]reflect.SelectCase, len(s.topics)+1)
		i := 0
		for _, t := range s.topics {
			cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(t.broadcastChannel)}
			topics[i] = t.topic
			i++
		}
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(s.newTopic)}
		return cases
	}
	cases := buildCases()
	for {
		chosen, value, ok := reflect.Select(cases)
		if !ok {
			log.Warn("An error occurred broadcasting the message")
			return
		}

		if chosen == len(cases)-1 {
			// Addition of a new topic
			cases = buildCases()
		} else {
			// Message on one of the existing topics
			// Gather all connections interested in this topic and send to them
			s.mux.Lock()
			topic := topics[chosen]
			wsconns := getConnListFromMap(s.topicMap[topic])
			s.mux.Unlock()
			s.broadcastToConnections(wsconns, value.Interface())
		}
	}
}

// getConnListFromMap is a simple helper to snapshot a map into a list, which can be called with a short-lived lock
func getConnListFromMap(tm map[string]*webSocketConnection) []*webSocketConnection {
	wsconns := make([]*webSocketConnection, 0, len(tm))
	for _, c := range tm {
		wsconns = append(wsconns, c)
	}
	return wsconns
}

func (s *webSocketServer) processReplies() {
	for {
		message := <-s.replyChannel
		s.mux.Lock()
		wsconns := getConnListFromMap(s.replyMap)
		s.mux.Unlock()
		s.broadcastToConnections(wsconns, message)
	}
}

func (s *webSocketServer) broadcastToConnections(connections []*webSocketConnection, message interface{}) {
	for _, c := range connections {
		select {
		case c.broadcast <- message:
		case <-c.closing:
			log.Warnf("Connection %s closed while attempting to deliver reply", c.id)
		}
	}
}
