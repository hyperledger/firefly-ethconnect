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

// TimeSorted base structure for time sortable things
type TimeSorted struct {
	CreatedISO8601 string `json:"created"`
}

// GetISO8601 returns an ISO8601 string
func (i *TimeSorted) GetISO8601() string {
	return i.CreatedISO8601
}

// IsLessThan performs reverse sorting by age, then forward sorting by ID
func (*TimeSorted) IsLessThan(i TimeSortable, j TimeSortable) bool {
	return i.GetISO8601() > j.GetISO8601() ||
		(i.GetISO8601() == j.GetISO8601() && i.GetID() < j.GetID())
}

// TimeSortable interface can be implemented by embedding TimeSorted and adding getID
type TimeSortable interface {
	IsLessThan(TimeSortable, TimeSortable) bool
	GetISO8601() string
	GetID() string
}
