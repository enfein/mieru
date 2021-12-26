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
	crand "crypto/rand"
	"encoding/binary"
	"sync"
	"time"

	"github.com/enfein/mieru/pkg/log"
)

// Cache is a global replay cache.
var Cache *ReplayCache = NewCache(600_000, 10*time.Minute)

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

	expireInterval time.Duration

	// salt mutates the entry before it is stored.
	salt []byte

	current  map[uint64]struct{}
	previous map[uint64]struct{}
}

// NewCache creates a new replay cache.
func NewCache(capacity int, expireInterval time.Duration) *ReplayCache {
	if capacity < 0 {
		log.Fatalf("replay cache capacity can't be negative")
	}
	if expireInterval.Nanoseconds() <= 0 {
		log.Fatalf("replay cache expire interval must be a positive time range")
	}
	var salt [8]byte
	for {
		if _, err := crand.Read(salt[:]); err != nil {
			log.Warnf("rand.Read() failed: %v, retrying...", err)
			time.Sleep(50 * time.Millisecond)
		} else {
			break
		}
	}
	return &ReplayCache{
		capacity:       capacity,
		expireTime:     time.Now().Add(expireInterval),
		expireInterval: expireInterval,
		salt:           salt[:],
		current:        make(map[uint64]struct{}),
		previous:       make(map[uint64]struct{}),
	}
}

// IsDuplicate checks if the given data is present in the replay cache.
func (c *ReplayCache) IsDuplicate(data []byte) bool {
	if c == nil || c.capacity == 0 {
		// The replay cache is disabled.
		return false
	}
	signature := c.computeSignature(data)
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.current) >= c.capacity || time.Now().After(c.expireTime) {
		c.previous = c.current
		c.current = make(map[uint64]struct{})
		c.expireTime = time.Now().Add(c.expireInterval)
	}

	if _, ok := c.current[signature]; ok {
		return true
	}
	c.current[signature] = struct{}{}
	if _, ok := c.previous[signature]; ok {
		return true
	}
	return false
}

// Sizes returns the number of entries in `current` map and `previous` map.
func (c *ReplayCache) Sizes() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.current), len(c.previous)
}

func (c *ReplayCache) computeSignature(data []byte) uint64 {
	signature := [8]byte{}
	for i, v := range c.salt {
		signature[i&0x7] ^= v
	}
	for i, v := range data {
		signature[i&0x7] ^= v
	}
	return binary.BigEndian.Uint64(signature[:])
}
