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
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
)

// WebSocketChannels is provided to allow us to do a blocking send to a namespace that will complete once a client connects on it
// We also provide a channel to listen on for closing of the connection, to allow a select to wake on a blocking send
type WebSocketChannels interface {
	GetChannels(topic string) (chan<- interface{}, chan<- interface{}, <-chan error, <-chan struct{})
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
	newTopic          chan bool
	upgrader          *websocket.Upgrader
	connections       map[string]*webSocketConnection
}

type webSocketTopic struct {
	topic            string
	senderChannel    chan interface{}
	broadcastChannel chan interface{}
	receiverChannel  chan error
	closingChannel   chan struct{}
}

// NewWebSocketServer create a new server with a simplified interface
func NewWebSocketServer() WebSocketServer {
	s := &webSocketServer{
		connections:       make(map[string]*webSocketConnection),
		topics:            make(map[string]*webSocketTopic),
		topicMap:          make(map[string]map[string]*webSocketConnection),
		newTopic:          make(chan bool),
		processingTimeout: 30 * time.Second,
		upgrader: &websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
	}
	go s.listenForBroadcasts()
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

func (s *webSocketServer) cycleTopic(t *webSocketTopic) {
	s.mux.Lock()
	defer s.mux.Unlock()

	// When a connection that was listening on a topic closes, we need to wake anyone
	// that was listening for a response
	close(t.closingChannel)
	t.closingChannel = make(chan struct{})
}

func (s *webSocketServer) connectionClosed(c *webSocketConnection) {
	s.mux.Lock()
	defer s.mux.Unlock()
	delete(s.connections, c.id)
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
	defer s.mux.Unlock()
	t, exists := s.topics[topic]
	if !exists {
		t = &webSocketTopic{
			topic:            topic,
			senderChannel:    make(chan interface{}),
			broadcastChannel: make(chan interface{}),
			receiverChannel:  make(chan error),
			closingChannel:   make(chan struct{}),
		}
		s.topics[topic] = t
		s.topicMap[topic] = make(map[string]*webSocketConnection)
		// Signal to the broadcaster that a new topic has been added
		s.newTopic <- true
	}
	return t
}

func (s *webSocketServer) GetChannels(topic string) (chan<- interface{}, chan<- interface{}, <-chan error, <-chan struct{}) {
	t := s.getTopic(topic)
	return t.senderChannel, t.broadcastChannel, t.receiverChannel, t.closingChannel
}

func (s *webSocketServer) ListenTopic(c *webSocketConnection, topic string) {
	// Track that this connection is interested in this topic
	s.topicMap[topic][c.id] = c
}

func (s *webSocketServer) listenForBroadcasts() {
	var topics []string
	buildCases := func() []reflect.SelectCase {
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
			topic := topics[chosen]
			for _, c := range s.topicMap[topic] {
				c.broadcast <- value.Interface()
			}
		}
	}
}
