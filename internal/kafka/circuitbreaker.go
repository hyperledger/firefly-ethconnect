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
	"math"
	"sync"
	"time"

	"github.com/hyperledger/firefly-ethconnect/internal/errors"
	log "github.com/sirupsen/logrus"
)

var (
	DefaultUpperBound     int64   = 80 * (1024 * 1024)
	DefaultResetThreshold float64 = 0.8
	DefaultLogFrequency           = 30 * time.Second
)

// CircuitBreakerConf defines the YAML config structure for the circuit breaker
type CircuitBreakerConf struct {
	Enabled         bool    `json:"enabled,omitempty"`
	UpperBound      int64   `json:"upperBound,omitempty"`
	ResetThreshold  float64 `json:"resetFraction,omitempty"`
	LogFrequencySec int     `json:"logFrequencySec,omitempty"`

	// calculated values
	logFrequency    time.Duration
	resetBufferSize int64
}

type CircuitBreaker interface {
	Update(topic string, partition int32, hwm, offset, size int64)
	Check(topic string) error
}

// cbPartitionState is a per-topic state structure for managing trips
type cbPartitionState struct {
	sizeEstimate int64
	offset       int64
	hwm          int64
	gap          int64
	bufSize      int64
	msgCount     int64
	msgBytes     int64
	tripped      bool
	lastTripTime time.Time
	lastLogged   time.Time
}

// circuitBreaker checks that the
type circuitBreaker struct {
	conf  *CircuitBreakerConf
	mux   sync.Mutex
	state map[string]map[int32]*cbPartitionState
}

var singletonCircuitBreaker *circuitBreaker

func InitCircuitBreaker(conf *CircuitBreakerConf) error {

	if conf == nil || !conf.Enabled {
		return nil
	}
	if singletonCircuitBreaker != nil {
		log.Warnf("Multiple Kafka Consumer CircuitBreaker configurations in config. Only the first will be applied")
		return nil
	}

	cb := &circuitBreaker{
		conf:  conf,
		state: make(map[string]map[int32]*cbPartitionState),
	}
	cb.conf.logFrequency = time.Duration(cb.conf.LogFrequencySec) * time.Second
	if cb.conf.logFrequency <= 0 {
		cb.conf.logFrequency = DefaultLogFrequency
	}
	if cb.conf.UpperBound <= 0 {
		cb.conf.UpperBound = DefaultUpperBound
	}
	if cb.conf.ResetThreshold <= 0 || cb.conf.ResetThreshold > 1 {
		cb.conf.ResetThreshold = DefaultResetThreshold
	}
	cb.conf.resetBufferSize = int64(math.Ceil(cb.conf.ResetThreshold * float64(cb.conf.UpperBound)))
	singletonCircuitBreaker = cb
	log.Infof("CircuitBreakerEnabled: trip=%.2fKb reset=%.2fKb", float64(cb.conf.UpperBound)/1024, float64(cb.conf.resetBufferSize)/1024)

	return nil
}

func GetCircuitBreaker() *circuitBreaker {
	return singletonCircuitBreaker
}

func (cb *circuitBreaker) logState(prefix, topic string, partition int32, partitionState *cbPartitionState) {
	lastTripped := partitionState.lastTripTime.Format(time.RFC3339Nano)
	if partitionState.lastTripTime.IsZero() {
		lastTripped = "never"
	}
	log.Infof("%s: topic=%s partition=%d offset=%d hwm=%d gap=%d gap.estimate=%.2fKb tripped=%t lastTripped=%s",
		prefix, topic, partition, partitionState.offset, partitionState.hwm, partitionState.gap, float64(partitionState.bufSize)/1024, partitionState.tripped, lastTripped)
}

func (cb *circuitBreaker) Update(topic string, partition int32, hwm, offset, size int64) {
	cb.mux.Lock()
	defer cb.mux.Unlock()

	topicState := cb.state[topic]
	if topicState == nil {
		topicState = make(map[int32]*cbPartitionState)
		cb.state[topic] = topicState
	}
	partitionState := topicState[partition]
	if partitionState == nil {
		partitionState = &cbPartitionState{
			msgCount:     1,
			msgBytes:     size,
			sizeEstimate: size,
		}
		topicState[partition] = partitionState
	} else {
		partitionState.msgCount++
		partitionState.msgBytes += size
		partitionState.sizeEstimate = partitionState.msgBytes / partitionState.msgCount
	}
	partitionState.hwm = hwm
	partitionState.offset = offset
	partitionState.gap = (hwm - offset)
	if partitionState.gap <= 0 {
		partitionState.gap = 0 // ensure positive
	}
	partitionState.bufSize = (partitionState.gap * partitionState.sizeEstimate)

	if partitionState.tripped {
		if partitionState.bufSize < cb.conf.resetBufferSize {
			partitionState.tripped = false
			cb.logState("CircuitBreakerReset", topic, partition, partitionState)
			return // No extra health log entry here
		}
	} else {
		if partitionState.bufSize > cb.conf.UpperBound {
			partitionState.tripped = true
			partitionState.lastTripTime = time.Now()
			cb.logState("CircuitBreakerTripped", topic, partition, partitionState)
			return // No extra health log entry here
		}
	}
	if time.Since(partitionState.lastLogged) > cb.conf.logFrequency {
		partitionState.lastLogged = time.Now()
		cb.logState("CircuitBreakerHealth", topic, partition, partitionState)
	}

}

func (cb *circuitBreaker) Check(topic string) error {
	cb.mux.Lock()
	defer cb.mux.Unlock()

	topicState := cb.state[topic]
	if topicState == nil {
		return nil
	}

	for partition, partitionState := range topicState {
		if partitionState.tripped {
			cb.logState("CircuitBreakerRejection", topic, partition, partitionState)
			return errors.Errorf(errors.CircuitBreakerTripped, partitionState.gap, float64(partitionState.bufSize)/1024)
		}
	}

	return nil
}
