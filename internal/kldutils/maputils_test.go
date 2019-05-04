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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetMapStringWithStringField(t *testing.T) {
	assert := assert.New(t)
	m := make(map[string]interface{})
	m["strkey"] = "strval"
	assert.Equal("strval", GetMapString(m, "strkey"))
}

func TestGetMapStringWithMissingField(t *testing.T) {
	assert := assert.New(t)
	m := make(map[string]interface{})
	assert.Equal("", GetMapString(m, "anykey"))
}

func TestGetMapStringWithNonStringField(t *testing.T) {
	assert := assert.New(t)
	m := make(map[string]interface{})
	m["intkey"] = 10
	assert.Equal("", GetMapString(m, "intkey"))
}
