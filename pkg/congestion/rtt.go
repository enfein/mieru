// MIT License
//
// Copyright (c) 2016 the quic-go authors & Google, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package congestion

import (
	"math"
	"time"

	"github.com/enfein/mieru/pkg/mathext"
)

const (
	rttAlpha          = 0.125
	oneMinusAlpha     = 1 - rttAlpha
	rttBeta           = 0.25
	oneMinusBeta      = 1 - rttBeta
	defaultInitialRTT = 500 * time.Millisecond
	infDuration       = time.Duration(math.MaxInt64)
)

// RTTStats provides round-trip statistics
type RTTStats struct {
	hasMeasurement bool

	minRTT        time.Duration
	latestRTT     time.Duration
	smoothedRTT   time.Duration
	meanDeviation time.Duration

	maxAckDelay   time.Duration
	rtoMultiplier float64
}

// NewRTTStats makes a properly initialized RTTStats object
func NewRTTStats() *RTTStats {
	return &RTTStats{
		rtoMultiplier: 1.0,
	}
}

// MinRTT Returns the minRTT for the entire connection.
// May return Zero if no valid updates have occurred.
func (r *RTTStats) MinRTT() time.Duration { return r.minRTT }

// LatestRTT returns the most recent rtt measurement.
// May return Zero if no valid updates have occurred.
func (r *RTTStats) LatestRTT() time.Duration { return r.latestRTT }

// SmoothedRTT returns the smoothed RTT for the connection.
// May return Zero if no valid updates have occurred.
func (r *RTTStats) SmoothedRTT() time.Duration { return r.smoothedRTT }

// MeanDeviation gets the mean deviation
func (r *RTTStats) MeanDeviation() time.Duration { return r.meanDeviation }

// MaxAckDelay gets the max_ack_delay advertised by the peer
func (r *RTTStats) MaxAckDelay() time.Duration { return r.maxAckDelay }

// RTO gets the retransmission timeout.
func (r *RTTStats) RTO() time.Duration {
	if r.SmoothedRTT() == 0 {
		return 2 * defaultInitialRTT
	}
	rto := r.SmoothedRTT() + mathext.Max(4*r.MeanDeviation(), 10*time.Millisecond)
	rto += r.MaxAckDelay()
	return time.Duration(float64(rto) * r.rtoMultiplier)
}

// UpdateRTT updates the RTT based on a new sample.
func (r *RTTStats) UpdateRTT(sample time.Duration) {
	if sample == infDuration || sample <= 0 {
		return
	}

	if r.minRTT == 0 || r.minRTT > sample {
		r.minRTT = sample
	}

	r.latestRTT = sample
	if !r.hasMeasurement {
		r.hasMeasurement = true
		r.smoothedRTT = sample
		r.meanDeviation = sample / 2
	} else {
		r.meanDeviation = time.Duration(oneMinusBeta*float32(r.meanDeviation/time.Microsecond)+rttBeta*float32(mathext.Abs(r.smoothedRTT-sample)/time.Microsecond)) * time.Microsecond
		r.smoothedRTT = time.Duration((float32(r.smoothedRTT/time.Microsecond)*oneMinusAlpha)+(float32(sample/time.Microsecond)*rttAlpha)) * time.Microsecond
	}
}

// SetMaxAckDelay sets the max_ack_delay
func (r *RTTStats) SetMaxAckDelay(mad time.Duration) {
	r.maxAckDelay = mad
}

// SetRTOMultiplier sets the retransmission timeout multiplier.
func (r *RTTStats) SetRTOMultiplier(n float64) {
	if n <= 0 {
		panic("retransmission timeout multiplier must be greater than 0")
	}
	r.rtoMultiplier = n
}

// SetInitialRTT sets the initial RTT.
// It is used during the 0-RTT handshake when restoring the RTT stats from the session state.
func (r *RTTStats) SetInitialRTT(t time.Duration) {
	if r.hasMeasurement {
		panic("initial RTT set after first measurement")
	}
	r.smoothedRTT = t
	r.latestRTT = t
}

// Reset is called when connection migrates and rtt measurement needs to be reset.
func (r *RTTStats) Reset() {
	r.latestRTT = 0
	r.minRTT = 0
	r.smoothedRTT = 0
	r.meanDeviation = 0
}

// ExpireSmoothedMetrics causes the smoothed_rtt to be increased to the latest_rtt if the latest_rtt
// is larger. The mean deviation is increased to the most recent deviation if
// it's larger.
func (r *RTTStats) ExpireSmoothedMetrics() {
	r.meanDeviation = mathext.Max(r.meanDeviation, mathext.Abs(r.smoothedRTT-r.latestRTT))
	r.smoothedRTT = mathext.Max(r.smoothedRTT, r.latestRTT)
}
