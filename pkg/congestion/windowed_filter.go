// Copyright (C) 2024  mieru authors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package congestion

import "github.com/enfein/mieru/v3/pkg/mathext"

// Implements Kathleen Nichols' algorithm for tracking the minimum (or maximum)
// estimate of a stream of samples over some fixed time interval. (E.g.,
// the minimum RTT over the past five minutes.) The algorithm keeps track of
// the best, second best, and third best min (or max) estimates, maintaining an
// invariant that the measurement time of the n'th best >= n-1'th best.

// The algorithm works as follows. On a reset, all three estimates are set to
// the same sample. The second best estimate is then recorded in the second
// quarter of the window, and a third best estimate is recorded in the second
// half of the window, bounding the worst case error when the true min is
// monotonically increasing (or true max is monotonically decreasing) over the
// window.
//
// A new best sample replaces all three estimates, since the new best is lower
// (or higher) than everything else in the window and it is the most recent.
// The window thus effectively gets reset on every new min. The same property
// holds true for second best and third best estimates. Specifically, when a
// sample arrives that is better than the second best but not better than the
// best, it replaces the second and third best estimates but not the best
// estimate. Similarly, a sample that is better than the third best estimate
// but not the other estimates replaces only the third best estimate.
//
// Finally, when the best expires, it is replaced by the second best, which in
// turn is replaced by the third best. The newest sample replaces the third
// best.

// WindowedFilter implements a windowed filter algorithm for tracking the minimum (or maximum)
// estimate of a stream of samples over some fixed time interval.
type WindowedFilter[V mathext.Number] struct {
	// windowLength is the period after which a best estimate expires.
	windowLength int64

	// zeroValue should be an invalid value for a true sample.
	zeroValue V

	estimates [3]Sample[V]
	compare   func(V, V) int
}

// Sample represents a sample with a value and a timestamp.
type Sample[V mathext.Number] struct {
	sample V
	time   int64
}

// NewWindowedFilter initializes a new WindowedFilter.
func NewWindowedFilter[V mathext.Number](windowLength int64, zeroValue V, compare func(V, V) int) *WindowedFilter[V] {
	return &WindowedFilter[V]{
		windowLength: windowLength,
		zeroValue:    zeroValue,
		estimates: [3]Sample[V]{
			{zeroValue, 0},
			{zeroValue, 0},
			{zeroValue, 0},
		},
		compare: compare,
	}
}

// SetWindowLength changes the window length. Does not update any current samples.
func (wf *WindowedFilter[V]) SetWindowLength(windowLength int64) {
	wf.windowLength = windowLength
}

// Update updates best estimates with newSample,
// and expires and updates best estimates as necessary.
func (wf *WindowedFilter[V]) Update(newSample V, newTime int64) {
	// Reset all estimates if they have not yet been initialized,
	// if new sample is a new best,
	// or if the newest recorded estimate is too old.
	if wf.compare(wf.estimates[0].sample, wf.zeroValue) == 0 || wf.compare(newSample, wf.estimates[0].sample) >= 0 || newTime-wf.estimates[2].time > wf.windowLength {
		wf.Reset(newSample, newTime)
		return
	}

	if wf.compare(newSample, wf.estimates[1].sample) >= 0 {
		wf.estimates[1] = Sample[V]{newSample, newTime}
		wf.estimates[2] = wf.estimates[1]
	} else if wf.compare(newSample, wf.estimates[2].sample) >= 0 {
		wf.estimates[2] = Sample[V]{newSample, newTime}
	}

	// Expire and update estimates as necessary.
	if newTime-wf.estimates[0].time > wf.windowLength {
		// The best estimate hasn't been updated for an entire window, so promote
		// second and third best estimates.
		wf.estimates[0] = wf.estimates[1]
		wf.estimates[1] = wf.estimates[2]
		wf.estimates[2] = Sample[V]{newSample, newTime}
		// Need to iterate one more time. Check if the new best estimate is
		// outside the window as well, since it may also have been recorded a
		// long time ago. Don't need to iterate once more since we cover that
		// case at the beginning of the method.
		if newTime-wf.estimates[0].time > wf.windowLength {
			wf.estimates[0] = wf.estimates[1]
			wf.estimates[1] = wf.estimates[2]
		}
		return
	}

	if wf.compare(wf.estimates[1].sample, wf.estimates[0].sample) == 0 && newTime-wf.estimates[1].time > wf.windowLength/4 {
		// A quarter of the window has passed without a better sample, so the
		// second-best estimate is taken from the second quarter of the window.
		wf.estimates[1] = Sample[V]{newSample, newTime}
		wf.estimates[2] = wf.estimates[1]
		return
	}

	if wf.compare(wf.estimates[2].sample, wf.estimates[1].sample) == 0 && newTime-wf.estimates[2].time > wf.windowLength/2 {
		// We've passed a half of the window without a better estimate, so take
		// a third-best estimate from the second half of the window.
		wf.estimates[2] = Sample[V]{newSample, newTime}
	}
}

// Reset resets all estimates to new sample.
func (wf *WindowedFilter[T]) Reset(newSample T, newTime int64) {
	wf.estimates[0] = Sample[T]{newSample, newTime}
	wf.estimates[1] = Sample[T]{newSample, newTime}
	wf.estimates[2] = Sample[T]{newSample, newTime}
}

// GetBest returns the best estimate.
func (wf *WindowedFilter[T]) GetBest() T {
	return wf.estimates[0].sample
}

// GetSecondBest returns the second best estimate.
func (wf *WindowedFilter[T]) GetSecondBest() T {
	return wf.estimates[1].sample
}

// GetThirdBest returns the third best estimate.
func (wf *WindowedFilter[T]) GetThirdBest() T {
	return wf.estimates[2].sample
}

// MinFilter compares two values and returns 1 if lhs is smaller,
// -1 if lhs is bigger, or 0 if two values are the same.
func MinFilter[T mathext.Number](lhs, rhs T) int {
	if lhs < rhs {
		return 1
	} else if lhs > rhs {
		return -1
	}
	return 0
}

// MaxFilter compares two values and returns 1 if lhs is bigger,
// -f if lhs is smaller, or 0 if two values are the same.
func MaxFilter[T mathext.Number](lhs, rhs T) int {
	if lhs > rhs {
		return 1
	} else if lhs < rhs {
		return -1
	}
	return 0
}
