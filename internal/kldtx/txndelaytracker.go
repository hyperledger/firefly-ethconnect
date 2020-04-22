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
// https://github.com/RobinUS2/golang-moving-average/blob/master/d.go

package kldtx

import (
	"time"

	log "github.com/sirupsen/logrus"
)

// Intention is that these values are a good baseline for the
// low-level exponential backoff retry of getting transaction
// receipts up to a maximum time. The maximum time is configured
// outside of this code, as when a transction receipt never
// comes back we cannot infer anything about the timing of
// successful transactions
const (
	// Window - Dataset size for moving average
	Window = 100
	// MinDelay - minimum delay before first try
	MinDelay = 100 * time.Millisecond
	// MaxDelay - maximum delay between tries
	MaxDelay = 10 * time.Second
	// Factor - exponential backoff factor
	Factor = 1.3
	// InitialDelayFraction - start with a large faction of the current average
	InitialDelayFraction = 0.8
	// FirstRetryFraction - then retry with exponential backoff using a fraction of that initial delay
	FirstRetryDelayFraction = 0.15
	// TracerFrequency - run an aggressive txn every X txns, and reset average
	TracerFrequency = 25
	// TracerDivisor - when a tracer runs, it runs at this division of the average
	TracerDivisor = 5
	// ResetThreshold - if a tracer comes in below this threshold of the average, we reset
	ResetThreshold = 0.3
)

// TxnDelayTracker - helps manage delays when checking for txn receipts
type TxnDelayTracker interface {
	GetInitialDelay() (delay time.Duration)
	GetRetryDelay(initialDelay time.Duration, retry int) (delay time.Duration)
	ReportSuccess(timeTaken time.Duration)
}

type txnDelayTracker struct {
	count       uint64
	window      int
	values      []float64
	valPos      int
	slotsFilled bool
}

// GetInitialDelay - returns an initial duration, which might be the
// current moving average, or might be artifically low to send in a
// tracer
func (d *txnDelayTracker) GetInitialDelay() (delay time.Duration) {
	// We start with a fraction of the average
	delay = time.Duration(int64(float64(d.avg())*InitialDelayFraction)) * time.Millisecond
	d.count++
	if (d.count % TracerFrequency) == 0 {
		log.Debugf("Sending tracer at count=%d delay=%.2fs", d.count, delay.Seconds())
		delay = delay / TracerDivisor
	}
	if delay < MinDelay {
		delay = MinDelay
	}
	if delay > MaxDelay {
		delay = MaxDelay
	}
	return delay
}

// GetRetryDelay - calculates the delay for a particular retry
func (d *txnDelayTracker) GetRetryDelay(initialDelay time.Duration, retry int) (delay time.Duration) {
	millis := FirstRetryDelayFraction * (float64(initialDelay.Nanoseconds()) / float64(time.Millisecond))
	for i := 0; i < retry; i++ {
		millis = millis * Factor
		delay = time.Duration(millis) * time.Millisecond
		if delay > MaxDelay {
			delay = MaxDelay
			break
		}
	}
	return delay
}

// ReportSuccess - adds a datapoint for how long a particular transaction took
func (d *txnDelayTracker) ReportSuccess(timeTaken time.Duration) {
	val := float64(timeTaken.Nanoseconds()) / float64(time.Millisecond)
	avg := d.avg()

	// If we find one of our tracers caused a big drop in the time it
	// took, then reset our average before we add the value
	factionOfAverage := (val / avg)
	if factionOfAverage <= ResetThreshold {
		log.Debugf("Tracer reset occurred val=%.2fs avg=%.2fs fact=%.2f threshold=%.2f",
			val, avg, factionOfAverage, ResetThreshold)
		d.reset()
	}

	// Average is in miliseconds
	d.add(val)
	log.Infof("Obtained receipt after %.2fms - average time to receipt: %.2fms", val, d.avg())
}

func (d *txnDelayTracker) avg() float64 {
	var sum = float64(0)
	var c = d.window - 1

	// Are all slots filled? If not, ignore unused
	if !d.slotsFilled {
		c = d.valPos - 1
		if c < 0 {
			// Empty register
			return 0
		}
	}

	// Sum values
	var ic = 0
	for i := 0; i <= c; i++ {
		sum += d.values[i]
		ic++
	}

	// Finalize average and return
	avg := sum / float64(ic)
	return avg
}

func (d *txnDelayTracker) add(val float64) {
	// Put into values array
	d.values[d.valPos] = val

	// Increment value position
	d.valPos = (d.valPos + 1) % d.window

	// Did we just go back to 0, effectively meaning we filled all registers?
	if !d.slotsFilled && d.valPos == 0 {
		d.slotsFilled = true
	}
}

func (d *txnDelayTracker) reset() {
	d.values = make([]float64, Window)
	d.valPos = 0
	d.slotsFilled = false
}

// NewTxnDelayTracker - constructs a new tracker
func NewTxnDelayTracker() TxnDelayTracker {
	d := &txnDelayTracker{
		window: Window,
	}
	d.reset()
	return d
}
