// Copyright 2018 Kaleido, a ConsenSys business

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
	uuid "github.com/nu7hatch/gouuid"
	log "github.com/sirupsen/logrus"
)

// UUIDv4 returns a new UUID V4 as a string
func UUIDv4() string {
	uuidV4, err := uuid.NewV4()
	if err != nil {
		log.Errorf("Failed to generate UUID for ClientID: %s", err)
		panic(err)
	}
	return uuidV4.String()
}
