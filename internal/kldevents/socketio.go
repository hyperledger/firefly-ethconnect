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

package kldevents

import "github.com/kaleido-io/ethconnect/internal/klderrors"

// SocketIoServerListener is provided to allow us to do a blocking send to a namespace that will complete once a client connects on it
type SocketIoServerListener interface {
	GetChannels(namespace string) (chan<- interface{}, <-chan error)
}

type socketIoAction struct {
	es   *eventStream
	spec *socketIoActionInfo
}

func newSocketIoAction(es *eventStream, spec *socketIoActionInfo) (*socketIoAction, error) {
	if es.socketIoListener == nil {
		return nil, klderrors.Errorf(klderrors.EventStreamsSocketIoNotConfigured)
	}
	return &socketIoAction{
		es:   es,
		spec: spec,
	}, nil
}

// attemptBatch attempts to deliver a batch over socket IO
func (w *socketIoAction) attemptBatch(batchNumber, attempt uint64, events []*eventData) error {

	// Get a blocking channel to send and receive on our chosen namespace
	sender, receiver := w.es.socketIoListener.GetChannels(w.spec.Namespace)

	// Sent the batch of events
	select {
	case sender <- events:
		break
	case <-w.es.updateInterrupt:
		return klderrors.Errorf(klderrors.EventStreamsSocketIoInterruptedSend)
	}

	// Wait for the next ack or exception
	var err error
	select {
	case err = <-receiver:
		break
	case <-w.es.updateInterrupt:
		return klderrors.Errorf(klderrors.EventStreamsSocketIoInterruptedReceive)
	}

	// Pass back any exception from the client
	return err
}
