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

package kldutils

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/stretchr/testify/assert"
)

func TestHTTPRequesterDoRequestBadURL(t *testing.T) {
	assert := assert.New(t)

	hr := NewHTTPRequester("unit test", &HTTPRequesterConf{})

	_, err := hr.DoRequest("GET", "! a URL", nil)
	assert.EqualError(err, "Error querying unit test")
}

func TestHTTPRequesterDoRequestBadPayload(t *testing.T) {
	assert := assert.New(t)

	hr := NewHTTPRequester("unit test", &HTTPRequesterConf{})

	bodyMap := make(map[string]interface{})
	bodyMap["unserializable"] = map[bool]interface{}{true: "JSON does not like this"}
	_, err := hr.DoRequest("GET", "http://localhost", bodyMap)
	assert.Regexp("Failed to serialize request payload", err.Error())
}

func TestHTTPRequesterErrorStatus(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(500)
		res.Write([]byte("{\"errorMessage\":\"poof\"}"))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	hr := NewHTTPRequester("unit test", &HTTPRequesterConf{})

	_, err := hr.DoRequest("GET", server.URL, nil)
	assert.EqualError(err, "unit test returned [500]: poof")
}

func TestHTTPRequesterUnknownError(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(500)
		res.Write([]byte("{\"bad\":\"ness\"}"))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	hr := NewHTTPRequester("unit test", &HTTPRequesterConf{})

	_, err := hr.DoRequest("GET", server.URL, nil)
	assert.EqualError(err, "Error querying unit test")
}

func TestHTTPRequesterPOSTSuccess(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.POST("/", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		var postBody map[string]interface{}
		json.NewDecoder(req.Body).Decode(&postBody)
		assert.Equal("body", postBody["some"])
		assert.Equal("headerval", req.Header.Get("someheader"))
		res.WriteHeader(200)
		res.Write([]byte("{\"some\":\"response\"}"))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	hr := NewHTTPRequester("unit test", &HTTPRequesterConf{
		Headers: map[string][]string{
			"someheader": []string{"headerval"},
		},
	})

	postBody := map[string]interface{}{
		"some": "body",
	}
	resBody, err := hr.DoRequest("POST", server.URL, postBody)
	assert.NoError(err)
	assert.Equal("response", resBody["some"])
}

func TestHTTPRequester404ToNil(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(404)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	hr := NewHTTPRequester("unit test", &HTTPRequesterConf{})

	resBody, err := hr.DoRequest("POST", server.URL, nil)
	assert.NoError(err)
	assert.Nil(resBody)
}

func TestHTTPRequester204ToEmpty(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(204)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	hr := NewHTTPRequester("unit test", &HTTPRequesterConf{})

	resBody, err := hr.DoRequest("GET", server.URL, nil)
	assert.NoError(err)
	assert.Empty(resBody)
}

func TestHTTPRequesterBadResponse(t *testing.T) {
	assert := assert.New(t)

	router := &httprouter.Router{}
	router.GET("/", func(res http.ResponseWriter, req *http.Request, parms httprouter.Params) {
		res.WriteHeader(200)
		res.Write([]byte("!JSON"))
	})
	server := httptest.NewServer(router)
	defer server.Close()

	hr := NewHTTPRequester("unit test", &HTTPRequesterConf{})

	_, err := hr.DoRequest("GET", server.URL, nil)
	assert.EqualError(err, "Could not process unit test [200] response")
}

func TestHTTPRequesterGetResponseStringVariants(t *testing.T) {
	assert := assert.New(t)

	hr := NewHTTPRequester("unit test", &HTTPRequesterConf{})

	body := map[string]interface{}{
		"not-a-string": false,
		"a-string":     "ok",
		"nil-value":    nil,
	}

	_, err := hr.GetResponseString(body, "non-existent", true)
	assert.EqualError(err, "'non-existent' missing in unit test response")

	_, err = hr.GetResponseString(body, "not-a-string", true)
	assert.EqualError(err, "'not-a-string' not a string in unit test response")

	str, err := hr.GetResponseString(body, "a-string", true)
	assert.NoError(err)
	assert.Equal("ok", str)

	str, err = hr.GetResponseString(body, "nil-value", true)
	assert.NoError(err)
	assert.Equal("", str)

	_, err = hr.GetResponseString(body, "nil-value", false)
	assert.EqualError(err, "'nil-value' empty (or null) in unit test response")

}
