// Copyright 2019 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package errors

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToRESTErrorFFEC(t *testing.T) {

	err := Errorf(ConfigFileReadFailed, "testfile.ext", fmt.Errorf("badness"))
	assert.Equal(t, ConfigFileReadFailed.Code(), err.Code())
	assert.Equal(t, "FFEC100003: Failed to read testfile.ext: badness", err.Error())
	assert.Equal(t, "FFEC100003: Failed to read testfile.ext: badness", err.String())
	restErr := ToRESTError(err)
	assert.Equal(t, "Failed to read testfile.ext: badness", restErr.Message)
	assert.Equal(t, "FFEC100003", restErr.Code)

}

func TestToRESTErrorOtherErr(t *testing.T) {

	err := fmt.Errorf("some: %s", "badness")
	assert.Equal(t, "some: badness", err.Error())
	restErr := ToRESTError(err)
	assert.Equal(t, "some: badness", restErr.Message)
	assert.Empty(t, restErr.Code)

}

func TestDuplicate(t *testing.T) {
	assert.Panics(t, func() {
		e(100000, "dup")
	})
}

func TestBadCode(t *testing.T) {
	assert.Panics(t, func() {
		e(0, "dup")
	})
}
