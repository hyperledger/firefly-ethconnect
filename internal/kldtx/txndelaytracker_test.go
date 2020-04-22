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

// Moving average logic builds upon
// https://github.com/RobinUS2/golang-moving-average/blob/master/ma.go
package kldtx

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	log "github.com/sirupsen/logrus"
)

func TestTxnDelayTrackerMovingAverageLogic(t *testing.T) {

	assert := assert.New(t)

	a := NewTxnDelayTracker().(*txnDelayTracker)
	a.window = 5
	assert.Equal(float64(0), a.avg())
	a.add(2)
	assert.True((a.avg() >= 1.999) && (a.avg() <= 2.001))
	a.add(4)
	a.add(2)
	assert.True((a.avg() >= 2.665) && (a.avg() <= 2.667))
	a.add(4)
	a.add(2)
	assert.True((a.avg() >= 2.799) && (a.avg() <= 2.801))

	// This one will go into the first slot again
	// evicting the first value
	a.add(10)
	assert.True((a.avg() >= 4.399) && (a.avg() <= 4.401))
}

func TestTxnDelayTrackerExponentialBackoff(t *testing.T) {

	assert := assert.New(t)

	d := NewTxnDelayTracker()
	d.ReportSuccess(5 * time.Second)
	initDelay := d.GetInitialDelay()

	var lastDelay time.Duration
	for i := 1; i <= 20; i++ {
		delay := d.GetRetryDelay(initDelay, i)
		log.Infof("InitDelay=%05.2fs FirstRetryFactor=%05.2f Factor=%05.2f Retry=%02d Delay=%05.2fs", initDelay.Seconds(), FirstRetryDelayFraction, Factor, i, delay.Seconds())
		assert.True(delay > lastDelay || delay == MaxDelay)
		lastDelay = delay
	}

	assert.Equal(MaxDelay, lastDelay)

}

func TestTxnDelayTrackerLimitMax(t *testing.T) {

	assert := assert.New(t)

	d := NewTxnDelayTracker()
	d.ReportSuccess(MaxDelay * 2)
	initDelay := d.GetInitialDelay()
	assert.Equal(MaxDelay, initDelay)

}

func TestTxnDelayTrackerTracerAndReset(t *testing.T) {

	log.SetLevel(log.DebugLevel)

	assert := assert.New(t)

	d := NewTxnDelayTracker()

	var delay time.Duration
	for i := 0; i < TracerFrequency-1; i++ {
		delay = d.GetInitialDelay()
		d.ReportSuccess(MinDelay * 100)
	}
	// Do a single digit precision check that we've pushed the average
	// to 100x the minimum
	normalInitDelay := time.Duration(float64(MinDelay) * 100 * InitialDelayFraction)
	assert.Equal(
		fmt.Sprintf("test1:%.1fs", normalInitDelay.Seconds()),
		fmt.Sprintf("test1:%.1fs", delay.Seconds()))
	highDelay := delay

	// Now send a tracer, which should be much lower
	delay = d.GetInitialDelay()
	assert.Equal(
		fmt.Sprintf("test2:%.1fs", (normalInitDelay/TracerDivisor).Seconds()),
		fmt.Sprintf("test2:%.1fs", delay.Seconds()))

	// Report success of that tracer to below the threshold,
	// and see that we've reset the average
	resetDuration := time.Duration(float64(highDelay.Nanoseconds()) * ResetThreshold * 0.99)
	d.ReportSuccess(resetDuration)
	delay = d.GetInitialDelay()
	assert.Equal(
		fmt.Sprintf("test3:%.1fs", time.Duration(float64(resetDuration)*InitialDelayFraction).Seconds()),
		fmt.Sprintf("test3:%.1fs", delay.Seconds()))
}
