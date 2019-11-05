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
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseHostsFileBadFile(t *testing.T) {
	assert := assert.New(t)
	_, err := ParseHosts("nonexistent")
	assert.Regexp("no such file or directory", err.Error())
}

func TestParseHostsGoodfile(t *testing.T) {
	assert := assert.New(t)
	hostMap, err := ParseHosts(path.Join("../../test/hosts_example"))
	assert.NoError(err)

	t.Logf("Hosts: %+v", hostMap)
	assert.Equal("127.0.0.2", hostMap["foo.bar"])
	assert.Equal("127.0.0.1", hostMap["localhost.local"])
	assert.Equal("", hostMap["should-not.resolve"])

}
