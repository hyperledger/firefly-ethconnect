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

package kldmessages

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type type1 struct {
	TimeSorted
	idField1 string
}

type type2 struct {
	TimeSorted
	idField2 string
}

func (t1 *type1) GetID() string {
	return t1.idField1
}

func (t2 *type2) GetID() string {
	return t2.idField2
}

func TestSortingAgeDecendingIDAscending(t *testing.T) {
	assert := assert.New(t)

	early := time.Now().UTC()
	late := time.Unix(early.Unix()+3600, 0).UTC()

	s1 := type1{
		TimeSorted: TimeSorted{
			CreatedISO8601: early.Format(time.RFC3339),
		},
		idField1: "zzzS1",
	}
	s2 := type1{
		TimeSorted: TimeSorted{
			CreatedISO8601: late.Format(time.RFC3339),
		},
		idField1: "aaaS2",
	}
	s3 := type2{
		TimeSorted: TimeSorted{
			CreatedISO8601: late.Format(time.RFC3339),
		},
		idField2: "zzzS3",
	}
	s4 := type2{
		TimeSorted: TimeSorted{
			CreatedISO8601: early.Format(time.RFC3339),
		},
		idField2: "aaaS4",
	}
	a1 := []TimeSortable{&s1, &s2, &s3, &s4}
	sort.Slice(a1, func(i, j int) bool {
		return a1[i].IsLessThan(a1[i], a1[j])
	})
	assert.Equal([]TimeSortable{&s2, &s3, &s4, &s1}, a1)
}
