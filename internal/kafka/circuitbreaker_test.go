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

package kafka

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCircuitBreakerInitDisabled(t *testing.T) {

	InitCircuitBreaker(&CircuitBreakerConf{})
	assert.Nil(t, singletonCircuitBreaker)

}

func TestCircuitBreakerInitDouble(t *testing.T) {

	InitCircuitBreaker(&CircuitBreakerConf{
		Enabled: true,
	})
	cb1 := singletonCircuitBreaker
	assert.NotNil(t, cb1)

	InitCircuitBreaker(&CircuitBreakerConf{
		Enabled: true,
	})
	assert.Equal(t, cb1, singletonCircuitBreaker)

	singletonCircuitBreaker = nil
}

func TestCircuitBreakerTrip(t *testing.T) {

	InitCircuitBreaker(&CircuitBreakerConf{
		Enabled:        true,
		UpperBound:     1024 * 1024,
		ResetThreshold: 0.5,
	})
	cb := GetCircuitBreaker()
	assert.Equal(t, int64(512*1024), cb.conf.resetBufferSize)

	cb.Update("topic1", 2, 10, 10, 1000)
	assert.Equal(t, int64(1000), cb.state["topic1"][2].sizeEstimate)
	assert.Zero(t, cb.state["topic1"][2].bufSize)
	assert.Zero(t, cb.state["topic1"][2].gap)
	assert.Nil(t, cb.Check("topic1"))

	cb.Update("topic1", 2, 2000, 1000, 1100)
	assert.Equal(t, int64((1000+1100)/2), cb.state["topic1"][2].sizeEstimate)
	assert.Equal(t, int64(1000*cb.state["topic1"][2].sizeEstimate), cb.state["topic1"][2].bufSize)
	assert.Equal(t, int64(1000), cb.state["topic1"][2].gap)
	assert.True(t, cb.state["topic1"][2].bufSize > cb.conf.UpperBound)
	assert.Regexp(t, "too large", cb.Check("topic1")) // tripped

	cb.Update("topic1", 2, 2000, 1500, 2048)
	assert.Equal(t, int64((1000+1100+2048)/3), cb.state["topic1"][2].sizeEstimate)
	assert.Equal(t, int64(500*cb.state["topic1"][2].sizeEstimate), cb.state["topic1"][2].bufSize)
	assert.Equal(t, int64(500), cb.state["topic1"][2].gap)
	assert.True(t, cb.state["topic1"][2].bufSize < cb.conf.UpperBound)
	assert.True(t, cb.state["topic1"][2].bufSize > cb.conf.resetBufferSize)
	assert.Regexp(t, "too large", cb.Check("topic1")) // tripped

	cb.Update("topic1", 2, 2000, 1800, 1024)
	assert.Equal(t, int64((1000+1100+2048+1024)/4), cb.state["topic1"][2].sizeEstimate)
	assert.Equal(t, int64(200*cb.state["topic1"][2].sizeEstimate), cb.state["topic1"][2].bufSize)
	assert.Equal(t, int64(200), cb.state["topic1"][2].gap)
	assert.True(t, cb.state["topic1"][2].bufSize < cb.conf.UpperBound)
	assert.True(t, cb.state["topic1"][2].bufSize < cb.conf.resetBufferSize)
	assert.Nil(t, cb.Check("topic1"))

	// Check other topics are isolated
	assert.Nil(t, cb.Check("topic2"))
	cb.Update("topic2", 0, 5000, 1000, 4096)
	assert.Regexp(t, "too large", cb.Check("topic2")) // tripped
	assert.Nil(t, cb.Check("topic1"))

	singletonCircuitBreaker = nil
}
