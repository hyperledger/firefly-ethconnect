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

package kldsio

import (
	"net/http"
	"sync"
	"time"

	sio "github.com/googollee/go-socket.io"
	engineio "github.com/googollee/go-socket.io/engineio"
	"github.com/kaleido-io/ethconnect/internal/klderrors"
	log "github.com/sirupsen/logrus"
)

// SocketIoServerListener is provided to allow us to do a blocking send to a namespace that will complete once a client connects on it
type SocketIoServerListener interface {
	RegisterNamespace(namespace string)
	GetChannels(namespace string) (chan<- interface{}, <-chan error)
}

// SocketIoServer is the full server interface with the init call
type SocketIoServer interface {
	SocketIoServerListener
	Init() (http.Handler, error)
	Close()
}

type socketIoConnTracker struct {
	closeNotify chan struct{}
	conn        sio.Conn
}

type socketIoNamespace struct {
	namespace       string
	connections     map[string]*socketIoConnTracker
	senderChannel   chan interface{}
	receiverChannel chan error
}

type socketIoServer struct {
	log        *log.Entry
	server     *sio.Server
	mux        sync.Mutex
	namespaces map[string]*socketIoNamespace
}

// NewSocketIoServer create a new server with a simplified interface
func NewSocketIoServer() SocketIoServer {
	return &socketIoServer{
		namespaces: make(map[string]*socketIoNamespace),
	}
}

func (s *socketIoServer) Init() (h http.Handler, err error) {
	s.server, err = sio.NewServer(&engineio.Options{})
	go s.server.Serve()
	return s.server, err
}

func (s *socketIoServer) handleNewConn(ns *socketIoNamespace, c sio.Conn) {
	s.mux.Lock()
	defer s.mux.Unlock()
	connTracker := &socketIoConnTracker{
		closeNotify: make(chan struct{}),
		conn:        c,
	}
	ns.connections[c.ID()] = connTracker
	go func() {
		for {
			select {
			case msg := <-ns.senderChannel:
				s.log.Debugf("Socket.io session '%s/%s' sending events", ns.namespace, c.ID())
				c.Emit("events", msg)
			case <-connTracker.closeNotify:
				s.log.Infof("Socket.io session '%s/%s' cleanup complete", ns.namespace, c.ID())
				return
			}
		}
	}()
}

func (s *socketIoServer) handleCloseConn(ns *socketIoNamespace, c sio.Conn) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if connTracker, exists := ns.connections[c.ID()]; exists {
		close(connTracker.closeNotify)
		delete(ns.connections, c.ID())
	}
}

func (s *socketIoServer) handleConnAck(ns *socketIoNamespace, c sio.Conn) {
	select {
	case <-time.After(30 * time.Second):
		s.log.Errorf("Socket.io session '%s/%s' attempted an 'ack'. We were not available to process it after 30 seconds. Closing connection", ns.namespace, c.ID())
		c.Close()
	case ns.receiverChannel <- nil:
		break
	}
}

func (s *socketIoServer) handleConnError(ns *socketIoNamespace, c sio.Conn, msg string) {
	select {
	case <-time.After(30 * time.Second):
		s.log.Errorf("Socket.io session '%s/%s' sent an 'error'. We were not available to process it after 30 seconds. Closing connection", ns.namespace, c.ID())
		c.Close()
	case ns.senderChannel <- klderrors.Errorf(klderrors.EventStreamsSocketIoErrorFromClient, msg):
		break
	}
}

func (s *socketIoServer) RegisterNamespace(namespace string) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if _, exists := s.namespaces[namespace]; !exists {
		ns := &socketIoNamespace{
			namespace:   namespace,
			connections: make(map[string]*socketIoConnTracker),
		}
		s.namespaces[namespace] = ns
		s.server.OnConnect(namespace, func(c sio.Conn) error {
			s.log.Infof("Socket.io session '%s/%s' connected", namespace, c.ID())
			s.handleNewConn(ns, c)
			return nil
		})
		s.server.OnDisconnect(namespace, func(c sio.Conn, msg string) {
			s.log.Infof("Socket.io session '%s/%s' disconnected: %s", namespace, c.ID(), msg)
			s.handleCloseConn(ns, c)
		})
		s.server.OnEvent(namespace, "ack", func(c sio.Conn) {
			s.log.Debugf("Socket.io session '%s/%s' received 'ack'", namespace, c.ID())
			s.handleConnAck(ns, c)
		})
		s.server.OnEvent(namespace, "error", func(c sio.Conn, msg string) {
			s.log.Errorf("Socket.io session '%s/%s' received 'error'", namespace, c.ID())
			s.handleConnError(ns, c, msg)
		})
	}
}

func (s *socketIoServer) Close() {
	s.server.Close()
}

func (s *socketIoServer) GetChannels(namespace string) (chan<- interface{}, <-chan error) {
	ns, exists := s.namespaces[namespace]
	if !exists {
		s.RegisterNamespace(namespace)
		ns, _ = s.namespaces[namespace]
	}
	return ns.senderChannel, ns.receiverChannel
}
