// Copyright 2018, 2021 Kaleido

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetenvOrDefault(t *testing.T) {
	os.Unsetenv("SOME_ENV_VAR")

	val := GetenvOrDefault("SOME_ENV_VAR", "DEFAULT_VAL")
	assert.Equal(t, "DEFAULT_VAL", val)

	os.Setenv("SOME_ENV_VAR", "SOME_VAL")

	val = GetenvOrDefault("SOME_ENV_VAR", "DEFAULT_VAL")
	assert.Equal(t, "SOME_VAL", val)
}

func TestGetenvOrDefaultUpperCase(t *testing.T) {
	os.Unsetenv("SOME_ENV_VAR")

	val := GetenvOrDefaultUpperCase("SOME_ENV_VAR", "default_Val")
	assert.Equal(t, "DEFAULT_VAL", val)

	os.Setenv("SOME_ENV_VAR", "some_VAL")

	val = GetenvOrDefaultUpperCase("SOME_ENV_VAR", "DEFAULT_VAL")
	assert.Equal(t, "SOME_VAL", val)
}

func TestGetenvOrDefaultLowerCase(t *testing.T) {
	os.Unsetenv("SOME_ENV_VAR")

	val := GetenvOrDefaultLowerCase("SOME_ENV_VAR", "DEFAULT_VAL")
	assert.Equal(t, "default_val", val)

	os.Setenv("SOME_ENV_VAR", "SOME_VAL")

	val = GetenvOrDefaultLowerCase("SOME_ENV_VAR", "DEFAULT_VAL")
	assert.Equal(t, "some_val", val)
}
