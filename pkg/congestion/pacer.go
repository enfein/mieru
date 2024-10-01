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

import (
	"time"

	"github.com/enfein/mieru/v3/pkg/mathext"
)

// Pacer limits the speed of sending packets.
// Pacer is not thread safe. The caller should provide synchronization
// to avoid race condition.
type Pacer struct {
	budgetAtLastSent int64
	maxBudget        int64 // determine the max burst
	minPacingRate    int64
	lastSentTime     time.Time
}

// NewPacer returns a new Pacer object.
func NewPacer(initialBudget, maxBudget, minPacingRate int64) *Pacer {
	if initialBudget <= 0 {
		panic("initial budget must be a positive number")
	}
	if maxBudget <= 0 {
		panic("max budget must be a positive number")
	}
	if maxBudget < initialBudget {
		panic("max budget is smaller than initial budget")
	}
	if minPacingRate <= 0 {
		panic("min pacing rate must be a positive number")
	}
	return &Pacer{
		budgetAtLastSent: initialBudget,
		maxBudget:        maxBudget,
		minPacingRate:    minPacingRate,
	}
}

// OnPacketSent updates the budget and time when a packet is sent.
func (p *Pacer) OnPacketSent(sentTime time.Time, bytes, pacingRate int64) {
	budget := p.Budget(sentTime, pacingRate)
	p.budgetAtLastSent = mathext.Max(budget-bytes, 0)
	p.lastSentTime = sentTime
}

// CanSend returns true if a packet can be sent based on the given pacing rate.
func (p *Pacer) CanSend(now time.Time, bytes, pacingRate int64) bool {
	return p.Budget(now, pacingRate) >= bytes
}

// Budget returns the maximum number of bytes can be sent right now.
func (p *Pacer) Budget(now time.Time, pacingRate int64) int64 {
	pacingRate = mathext.Max(pacingRate, p.minPacingRate)
	if p.lastSentTime.IsZero() {
		return p.budgetAtLastSent
	}
	budget := p.budgetAtLastSent + (pacingRate*now.Sub(p.lastSentTime).Nanoseconds())/int64(time.Second)
	if budget < 0 {
		// overflow
		return p.maxBudget
	}
	return mathext.Min(budget, p.maxBudget)
}
