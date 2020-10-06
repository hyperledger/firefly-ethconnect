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
	"net/http/httptest"
	"net/url"
	"testing"

	gosocketio "github.com/ambelovsky/gosf-socketio"
	"github.com/ambelovsky/gosf-socketio/transport"
	"github.com/stretchr/testify/assert"
)

func newTestSocketIoServer() (*socketIoServer, *httptest.Server, error) {
	sio := NewSocketIoServer().(*socketIoServer)
	server, err := sio.Init()
	ts := httptest.NewServer(server)
	return sio, ts, err
}

func TestConnectSendReceiveCycle(t *testing.T) {
	assert := assert.New(t)

	sio, ts, err := newTestSocketIoServer()
	defer ts.Close()
	defer sio.Close()
	assert.NoError(err)

	u, err := url.Parse(ts.URL)
	u.Scheme = "ws"
	c, err := gosocketio.Dial(
		u.String(),
		transport.GetDefaultWebsocketTransport(),
	)
	assert.NoError(err)

	c.On("events", func(msg string) {
		t.Logf("Received 'events' message: %s", msg)
		c.Emit("ack", nil)
	})

	s, r := sio.GetChannels("")

	s <- "Hello World"
	err = <-r

	assert.NoError(err)

}
