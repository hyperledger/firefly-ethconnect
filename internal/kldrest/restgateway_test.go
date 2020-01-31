// Copyright 2018, 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kldrest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/kaleido-io/ethconnect/internal/kldauth"
	"github.com/kaleido-io/ethconnect/internal/kldauth/kldauthtest"
	"github.com/stretchr/testify/assert"
)

var lastPort = 9000

func TestNewRESTGateway(t *testing.T) {
	assert := assert.New(t)
	var printYAML = false
	g := NewRESTGateway(&printYAML)
	var conf RESTGatewayConf
	conf.HTTP.LocalAddr = "127.0.0.1"
	g.SetConf(&conf)
	assert.Equal("127.0.0.1", g.Conf().HTTP.LocalAddr)
}

func TestValidateConfInvalidArgs(t *testing.T) {
	assert := assert.New(t)
	var printYAML = false
	g := NewRESTGateway(&printYAML)
	g.conf.MongoDB.URL = "mongodb://localhost:27017"
	err := g.ValidateConf()
	assert.EqualError(err, "MongoDB URL, Database and Collection name must be specified to enable the receipt store")
}

func TestValidateConfInvalidOpenAPIArgs(t *testing.T) {
	assert := assert.New(t)
	var printYAML = false
	g := NewRESTGateway(&printYAML)
	g.conf.OpenAPI.StoragePath = "/tmp/t"
	err := g.ValidateConf()
	assert.EqualError(err, "RPC URL and Storage Path must be supplied to enable the Open API REST Gateway")
}

func TestStartStatusStopNoKafkaWebhooksAccessToken(t *testing.T) {
	assert := assert.New(t)

	kldauth.RegisterSecurityModule(&kldauthtest.TestSecurityModule{})
	router := &httprouter.Router{}
	fakeRPC := httptest.NewServer(router)
	// Add username/pass to confirm we don't log
	u, _ := url.Parse(fakeRPC.URL)
	u.User = url.UserPassword("user1", "pass1")

	var printYAML = false
	g := NewRESTGateway(&printYAML)
	g.conf.HTTP.Port = lastPort
	g.conf.HTTP.LocalAddr = "127.0.0.1"
	g.conf.RPC.URL = u.String()
	g.conf.OpenAPI.StoragePath = "/tmp/t"
	lastPort++
	var err error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err = g.Start()
		wg.Done()
	}()

	url, _ := url.Parse(fmt.Sprintf("http://localhost:%d/status", g.conf.HTTP.Port))
	var resp *http.Response
	for i := 0; i < 5; i++ {
		time.Sleep(200 * time.Millisecond)
		req := &http.Request{URL: url, Method: http.MethodGet, Header: http.Header{
			"AUTHORIZATION": []string{"BeaRER testat"},
		}}
		resp, err = http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == 200 {
			break
		}
	}
	assert.NoError(err)
	assert.Equal(200, resp.StatusCode)
	var statusResp statusMsg
	err = json.NewDecoder(resp.Body).Decode(&statusResp)
	assert.Equal(true, statusResp.OK)

	g.srv.Close()
	wg.Wait()
	assert.EqualError(err, "http: Server closed")

	kldauth.RegisterSecurityModule(nil)

}

func TestStartStatusStopNoKafkaWebhooksMissingToken(t *testing.T) {
	assert := assert.New(t)

	kldauth.RegisterSecurityModule(&kldauthtest.TestSecurityModule{})

	router := &httprouter.Router{}
	fakeRPC := httptest.NewServer(router)
	// Add username/pass to confirm we don't log
	u, _ := url.Parse(fakeRPC.URL)
	u.User = url.UserPassword("user1", "pass1")

	var printYAML = false
	g := NewRESTGateway(&printYAML)
	g.conf.HTTP.Port = lastPort
	g.conf.HTTP.LocalAddr = "127.0.0.1"
	g.conf.RPC.URL = u.String()
	g.conf.OpenAPI.StoragePath = "/tmp/t"
	lastPort++
	var err error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err = g.Start()
		wg.Done()
	}()

	url, _ := url.Parse(fmt.Sprintf("http://localhost:%d/status", g.conf.HTTP.Port))
	var resp *http.Response
	for i := 0; i < 5; i++ {
		time.Sleep(200 * time.Millisecond)
		req := &http.Request{URL: url, Method: http.MethodGet, Header: http.Header{
			"authorization": []string{"bearer"},
		}}
		resp, err = http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == 401 {
			break
		}
	}
	assert.NoError(err)
	assert.Equal(401, resp.StatusCode)
	var errResp errMsg
	err = json.NewDecoder(resp.Body).Decode(&errResp)
	assert.Equal("Unauthorized", errResp.Message)

	g.srv.Close()
	wg.Wait()
	assert.EqualError(err, "http: Server closed")

	kldauth.RegisterSecurityModule(nil)

}

func TestStartWithKafkaWebhooks(t *testing.T) {
	assert := assert.New(t)

	var printYAML = false
	g := NewRESTGateway(&printYAML)
	g.conf.HTTP.Port = lastPort
	g.conf.HTTP.LocalAddr = "127.0.0.1"
	g.conf.Kafka.Brokers = []string{""}
	lastPort++
	var err error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err = g.Start()
		wg.Done()
	}()

	wg.Wait()
	assert.EqualError(err, "No Kafka brokers configured")
}

func TestStartWithBadTLS(t *testing.T) {
	assert := assert.New(t)

	var printYAML = false
	g := NewRESTGateway(&printYAML)
	g.conf.HTTP.Port = lastPort
	g.conf.HTTP.LocalAddr = "127.0.0.1"
	g.conf.HTTP.TLS.Enabled = true
	g.conf.HTTP.TLS.ClientKeyFile = "incomplete config"
	lastPort++
	var err error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err = g.Start()
		wg.Done()
	}()

	wg.Wait()
	assert.EqualError(err, "Client private key and certificate must both be provided for mutual auth")
}

func TestStartInvalidMongo(t *testing.T) {
	assert := assert.New(t)

	fakeRouter := &httprouter.Router{}
	fakeMongo := httptest.NewServer(fakeRouter)
	defer fakeMongo.Close()

	var printYAML = false
	g := NewRESTGateway(&printYAML)
	url, _ := url.Parse(fakeMongo.URL)
	url.Scheme = "mongodb"
	g.conf.MongoDB.URL = url.String()
	g.conf.MongoDB.ConnectTimeoutMS = 100
	err := g.Start()
	assert.EqualError(err, "Unable to connect to MongoDB: no reachable servers")
}

func TestStartWithBadRPCUrl(t *testing.T) {
	assert := assert.New(t)

	var printYAML = false
	g := NewRESTGateway(&printYAML)
	g.conf.HTTP.Port = lastPort
	g.conf.HTTP.LocalAddr = "127.0.0.1"
	g.conf.OpenAPI.StoragePath = "/tmp/t"
	lastPort++
	var err error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err = g.Start()
		wg.Done()
	}()

	wg.Wait()
	assert.EqualError(err, "JSON/RPC connection to  failed: dial unix: missing address")
}
func TestPrintYaml(t *testing.T) {
	assert := assert.New(t)

	var printYAML = true
	g := NewRESTGateway(&printYAML)
	g.printYAML = &printYAML
	cmd := g.CobraInit("rest")
	cmd.SetArgs([]string{"-l", "8001", "-r", "http://localhost:8545"})
	err := cmd.Execute()
	assert.Nil(err)
}

func TestMissingRPCAndMissingKafka(t *testing.T) {
	assert := assert.New(t)

	var printYAML = true
	g := NewRESTGateway(&printYAML)
	g.printYAML = &printYAML
	cmd := g.CobraInit("rest")
	cmd.SetArgs([]string{"-l", "8001"})
	err := cmd.Execute()
	assert.EqualError(err, "No JSON/RPC URL set for ethereum node")
}

func TestMaxWaitTimeTooSmallWarns(t *testing.T) {
	assert := assert.New(t)

	var printYAML = true
	g := NewRESTGateway(&printYAML)
	g.printYAML = &printYAML
	cmd := g.CobraInit("rest")
	cmd.SetArgs([]string{"-l", "8001", "-r", "http://localhost:8545", "-x", "1"})
	err := cmd.Execute()
	assert.NoError(err)
}

func TestKafkaCobraInitSuccess(t *testing.T) {
	assert := assert.New(t)

	var printYAML = true
	g := NewRESTGateway(&printYAML)
	g.printYAML = &printYAML
	cmd := g.CobraInit("rest")
	args := []string{
		"-l", "8001",
		"-b", "broker1", "-b", "broker2",
		"-t", "topic1", "-T", "topic2",
		"-g", "group1",
	}
	cmd.SetArgs(args)
	cmd.ParseFlags(args)
	err := cmd.PreRunE(cmd, args)
	assert.Nil(err)
	assert.Equal([]string{"broker1", "broker2"}, g.conf.Kafka.Brokers)
}

func TestKafkaCobraInitFailure(t *testing.T) {
	assert := assert.New(t)

	var printYAML = true
	g := NewRESTGateway(&printYAML)
	g.printYAML = &printYAML
	cmd := g.CobraInit("rest")
	args := []string{
		"-b", "broker1", "-b", "broker2",
	}
	cmd.SetArgs(args)
	cmd.ParseFlags(args)
	err := cmd.PreRunE(cmd, args)
	assert.EqualError(err, "No output topic specified for bridge to send events to")
	assert.Equal([]string{"broker1", "broker2"}, g.conf.Kafka.Brokers)
}

func TestDispatchMsgAsyncPassesThroughToWebhooks(t *testing.T) {
	assert := assert.New(t)

	var printYAML = true
	g := NewRESTGateway(&printYAML)
	fakeHandler := &mockHandler{}
	g.webhooks = newWebhooks(fakeHandler, nil)

	var fakeMsg map[string]interface{}
	_, err := g.DispatchMsgAsync(context.Background(), fakeMsg, true)
	assert.EqualError(err, "Invalid message - missing 'headers' (or not an object)")
}
