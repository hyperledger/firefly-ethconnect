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
	"strings"
)

func GetenvOrDefault(varName, defaultVal string) string {
	val := os.Getenv(varName)
	if val == "" {
		return defaultVal
	}
	return val
}

func GetenvOrDefaultUpperCase(varName, defaultVal string) string {
	val := GetenvOrDefault(varName, defaultVal)
	return strings.ToUpper(val)
}

func GetenvOrDefaultLowerCase(varName, defaultVal string) string {
	val := GetenvOrDefault(varName, defaultVal)
	return strings.ToLower(val)
}
