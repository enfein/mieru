// Copyright (C) 2021  mieru authors
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

package replay

import (
	"encoding/binary"
	"sync"
	"time"

	"github.com/enfein/mieru/pkg/metrics"
)

const (
	EmptyTag = ""
)

var (
	// Number of replay packets sent from a new session.
	NewSession = metrics.RegisterMetric("replay", "NewSession", metrics.COUNTER)

	// Number of replay packets sent from a known session.
	KnownSession = metrics.RegisterMetric("replay", "KnownSession", metrics.COUNTER)
)

// ReplayCache stores the signature of recent decrypted packets to avoid
// a replay attack.
type ReplayCache struct {
	// mu is required to access the replay cache.
	mu sync.Mutex

	// capacity is the maximum number of entries in one replay cache.
	// If the size of `current` map is bigger than capacity, `current` map
	// will replace `previous` map.
	capacity int

	// expireTime is the time when `current` map should replace `previous` map,
	// regardless of the number of entries in the `current` map.
	expireTime time.Time

	// expireInterval is the interval to reset expireTime.
	expireInterval time.Duration

	// current stores the current set of packet signatures.
	current map[uint64]string

	// previous stores the previous set of packet signatures.
	previous map[uint64]string
}

// NewCache creates a new replay cache.
func NewCache(capacity int, expireInterval time.Duration) *ReplayCache {
	if capacity < 0 {
		panic("replay cache capacity can't be negative")
	}
	if expireInterval.Nanoseconds() <= 0 {
		panic("replay cache expire interval must be a positive time range")
	}
	return &ReplayCache{
		capacity:       capacity,
		expireTime:     time.Now().Add(expireInterval),
		expireInterval: expireInterval,
		current:        make(map[uint64]string),
		previous:       make(map[uint64]string),
	}
}

// IsDuplicate checks if the given data is present in the replay cache.
// If a non-empty tag is provided, it is not considered as a duplicate
// when the same tag is used. This can allow retransmit exact content
// from the same network address. Use an empty tag disables this feature.
func (c *ReplayCache) IsDuplicate(data []byte, tag string) bool {
	if c == nil || c.capacity == 0 {
		// The replay cache is disabled.
		return false
	}
	signature := c.computeSignature(data)
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.current) >= c.capacity || time.Now().After(c.expireTime) {
		c.previous = c.current
		c.current = make(map[uint64]string)
		c.expireTime = time.Now().Add(c.expireInterval)
	}

	if existingTag, ok := c.current[signature]; ok {
		if existingTag == EmptyTag || tag == EmptyTag {
			return true
		}
		return existingTag != tag
	} else {
		c.current[signature] = tag
	}
	if existingTag, ok := c.previous[signature]; ok {
		if existingTag == EmptyTag || tag == EmptyTag {
			return true
		}
		return existingTag != tag
	}
	return false
}

// Sizes returns the number of entries in `current` map and `previous` map.
func (c *ReplayCache) Sizes() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.current), len(c.previous)
}

// Clear removes all the data in the replay cache.
func (c *ReplayCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = make(map[uint64]string)
	c.previous = make(map[uint64]string)
}

func (c *ReplayCache) computeSignature(data []byte) uint64 {
	signature := [8]byte{}
	for i, v := range data {
		signature[i&0x7] ^= v
	}
	return binary.BigEndian.Uint64(signature[:])
}
