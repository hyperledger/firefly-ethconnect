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
	"bytes"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

type errReader int

func (errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("pop")
}

func TestYAMLorJSONPayloadGoodJSON(t *testing.T) {
	assert := assert.New(t)

	req := httptest.NewRequest("POST", "/anything", bytes.NewReader([]byte("{\"hello\":\"world\"}")))

	v, err := YAMLorJSONPayload(req)
	assert.NoError(err)
	assert.Equal("world", v["hello"])
}

func TestYAMLorJSONPayloadGoodYAML(t *testing.T) {
	assert := assert.New(t)

	req := httptest.NewRequest("POST", "/anything", bytes.NewReader([]byte("hello: world")))

	v, err := YAMLorJSONPayload(req)
	assert.NoError(err)
	assert.Equal("world", v["hello"])
}

func TestYAMLorJSONPayloadUnparsable(t *testing.T) {
	assert := assert.New(t)

	req := httptest.NewRequest("POST", "/anything", bytes.NewReader([]byte(": not going to happen")))

	_, err := YAMLorJSONPayload(req)
	assert.Regexp("Unable to parse as YAML or JSON", err.Error())
}

func TestYAMLorJSONPayloadTooBig(t *testing.T) {
	assert := assert.New(t)

	bigBytes := make([]byte, 1025*1024)
	req := httptest.NewRequest("POST", "/anything", bytes.NewReader(bigBytes))

	_, err := YAMLorJSONPayload(req)
	assert.EqualError(err, "Message exceeds maximum allowable size")
}

func TestYAMLorJSONPayloadReadError(t *testing.T) {
	assert := assert.New(t)

	req := httptest.NewRequest("POST", "/anything", errReader(0))

	_, err := YAMLorJSONPayload(req)
	assert.Regexp("Unable to read input data", err.Error())
}
