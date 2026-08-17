// Copyright (C) 2026  mieru authors
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

package serveruser

import (
	"hash/maphash"
	"net"
	"net/netip"
	"time"
)

const (
	sourceUserCacheLifeSeconds = 10 * 60
)

var (
	sourceUserCacheHashSeed     = maphash.MakeSeed()
	sourceUserCacheProcessStart = time.Now()
)

type sourceUserCacheTickFunc func() uint32

type sourceUserCacheCandidate struct {
	id  uint32
	age uint32
}

type sourceUserCacheWaySelection struct {
	way     int
	entry   *sourceUserCacheEntry
	expired bool
}

type sourceUserCacheRecordTransition struct {
	inserted bool
	expired  bool
	evicted  bool
}

func sourceUserCacheCurrentTick() uint32 {
	return uint32(time.Since(sourceUserCacheProcessStart) / time.Second)
}

func newSourceUserCacheWithTick(stats *sourceUserCacheStats, tick sourceUserCacheTickFunc) *sourceUserCache {
	if tick == nil {
		tick = sourceUserCacheCurrentTick
	}
	c := &sourceUserCache{stats: stats, tick: tick}
	c.table.Store(&sourceUserCacheTable{})
	return c
}

// sourceUserCacheKey returns a canonical address key without formatting the
// address as text. Ports and IPv6 zone identifiers are intentionally omitted.
// IPv4 and IPv4-mapped IPv6 addresses have the same key.
func sourceUserCacheKey(addr net.Addr) ([16]byte, bool) {
	var ip net.IP
	switch addr := addr.(type) {
	case *net.TCPAddr:
		if addr == nil {
			return [16]byte{}, false
		}
		ip = addr.IP
	case *net.UDPAddr:
		if addr == nil {
			return [16]byte{}, false
		}
		ip = addr.IP
	default:
		return [16]byte{}, false
	}

	parsed, ok := netip.AddrFromSlice(ip)
	if !ok || !parsed.IsValid() {
		return [16]byte{}, false
	}
	return parsed.Unmap().As16(), true
}

// SourceFromAddr returns a cache source derived from an IP address. Ports and
// IPv6 zones are intentionally ignored. Unsupported or invalid addresses
// produce a zero Source, which disables cache lookup.
func SourceFromAddr(addr net.Addr) Source {
	key, valid := sourceUserCacheKey(addr)
	return Source{key: key, valid: valid}
}

func sourceUserCacheBucketIndex(key [16]byte) uint32 {
	return uint32(maphash.Bytes(sourceUserCacheHashSeed, key[:])) & (sourceUserCacheBucketCount - 1)
}

func sourceUserCachePackUser(userID, tick uint32) uint64 {
	return uint64(userID)<<32 | uint64(tick)
}

func sourceUserCacheUnpackUser(packed uint64) (uint32, uint32) {
	return uint32(packed >> 32), uint32(packed)
}

func sourceUserCacheExpired(now, then uint32) bool {
	return sourceUserCacheAge(now, then) >= sourceUserCacheLifeSeconds
}

// sourceUserCacheAge uses unsigned subtraction so elapsed time remains correct
// when the 32-bit monotonic tick wraps around. A lookup that observes a tick
// written after its snapshot of now may report a harmless transient miss.
func sourceUserCacheAge(now, then uint32) uint32 {
	return now - then
}

func (c *sourceUserCache) currentTick() uint32 {
	if c == nil || c.tick == nil {
		return sourceUserCacheCurrentTick()
	}
	return c.tick()
}

// lookup returns cached user IDs for the lookup key in most recently used order.
// Expiry is logical: expired entries remain physically present until reused.
func (c *sourceUserCache) lookup(key [16]byte) ([sourceUserCacheUsers]uint32, int) {
	var result [sourceUserCacheUsers]uint32
	if c == nil {
		return result, 0
	}
	if c.stats != nil {
		c.stats.lookups.Add(1)
	}
	table := c.loadTable()
	if table == nil {
		if c.stats != nil {
			c.stats.sourceMisses.Add(1)
		}
		return result, 0
	}

	now := c.currentTick()
	bucket := &table.buckets[sourceUserCacheBucketIndex(key)]
	for way := 0; way < sourceUserCacheWays; way++ {
		entry := bucket.ways[way].Load()
		if entry == nil || entry.key != key {
			continue
		}
		// Entry found but stale; stop searching other ways.
		if sourceUserCacheExpired(now, entry.lastActive.Load()) {
			break
		}

		var candidates [sourceUserCacheUsers]sourceUserCacheCandidate
		count := 0
		for i := 0; i < sourceUserCacheUsers; i++ {
			userID, seen := sourceUserCacheUnpackUser(entry.users[i].Load())
			if userID == 0 || sourceUserCacheExpired(now, seen) {
				continue
			}
			age := sourceUserCacheAge(now, seen)

			// Deduplicate: a user may occupy multiple slots;
			// keep the freshest (smallest age) seen so far.
			duplicate := -1
			for j := 0; j < count; j++ {
				if candidates[j].id == userID {
					duplicate = j
					break
				}
			}
			if duplicate >= 0 {
				if age < candidates[duplicate].age {
					candidates[duplicate].age = age
				}
				continue
			}
			candidates[count] = sourceUserCacheCandidate{id: userID, age: age}
			count++
		}

		// Insertion-sort candidates by age so the most recently
		// seen user is tried first by the caller.
		for i := 1; i < count; i++ {
			candidate := candidates[i]
			j := i
			for j > 0 && candidate.age < candidates[j-1].age {
				candidates[j] = candidates[j-1]
				j--
			}
			candidates[j] = candidate
		}
		for i := 0; i < count; i++ {
			result[i] = candidates[i].id
		}
		if count > 0 {
			if c.stats != nil {
				c.stats.sourceHits.Add(1)
			}
			return result, count
		}
		break // At most one way can match the key; no need to check others.
	}

	if c.stats != nil {
		c.stats.sourceMisses.Add(1)
	}
	return result, 0
}

// recordAuthenticated records a source to user association after the caller
// has authenticated the user. A zero user ID is reserved and is ignored.
func (c *sourceUserCache) recordAuthenticated(key [16]byte, userID uint32) {
	if c == nil || userID == 0 {
		return
	}
	table := c.loadTable()
	if table == nil {
		return
	}
	c.recordAuthenticatedInTable(table, key, userID)
}

// recordAuthenticatedInTable continues a record operation after its one table
// load. Retirement may detach the table concurrently; an operation that
// already loaded it may still complete and update the Mux-lifetime counters.
func (c *sourceUserCache) recordAuthenticatedInTable(table *sourceUserCacheTable, key [16]byte, userID uint32) {
	if c == nil || table == nil || userID == 0 {
		return
	}

	bucketIndex := sourceUserCacheBucketIndex(key)
	bucket := &table.buckets[bucketIndex]

	// All writers for a bucket share a lock. This couples same-key lookup,
	// activity refresh, victim selection, and pointer replacement while keeping
	// lookups lock-free.
	bucketLock := &c.bucketLocks[bucketIndex&(sourceUserCacheLockStripes-1)]
	bucketLock.Lock()
	defer bucketLock.Unlock()

	now := c.currentTick()
	var ways [sourceUserCacheWays]*sourceUserCacheEntry
	match := -1
	for way := 0; way < sourceUserCacheWays; way++ {
		ways[way] = bucket.ways[way].Load()
		if ways[way] != nil && ways[way].key == key && match < 0 {
			match = way
		}
	}

	if match >= 0 {
		entry := ways[match]
		sourceExpired := sourceUserCacheExpired(now, entry.lastActive.Load())
		transition := c.recordUser(entry, userID, now)
		if sourceExpired {
			// The complete source was logically absent before this record, even
			// if an individual slot happened to contain the same user ID.
			transition.inserted = true
			transition.expired = true
			transition.evicted = false
		}
		// Writers for this bucket are serialized by bucketLock. Publish the
		// current tick directly so numeric wraparound can't suppress a refresh.
		entry.lastActive.Store(now)
		c.recordTransition(transition)
		return
	}

	selection := selectSourceUserCacheWay(ways, now)
	replacement := &sourceUserCacheEntry{key: key}
	replacement.lastActive.Store(now)
	replacement.users[0].Store(sourceUserCachePackUser(userID, now))
	bucket.ways[selection.way].Store(replacement)
	c.recordTransition(sourceUserCacheRecordTransition{
		inserted: true,
		expired:  selection.expired,
		evicted:  selection.entry != nil && !selection.expired,
	})
}

// recordUser refreshes userID or inserts it into a deterministic user slot.
// It reports only completed state transitions; a live same-user refresh is not
// an insertion, expiry, or eviction.
func (c *sourceUserCache) recordUser(entry *sourceUserCacheEntry, userID, now uint32) sourceUserCacheRecordTransition {
	for {
		var slots [sourceUserCacheUsers]uint64
		same := -1
		empty := -1
		expired := -1
		oldest := -1
		var oldestAge uint32
		for i := 0; i < sourceUserCacheUsers; i++ {
			slots[i] = entry.users[i].Load()
			id, seen := sourceUserCacheUnpackUser(slots[i])
			switch {
			case id == userID && same < 0:
				same = i
			case id == 0 && empty < 0:
				empty = i
			case id != 0 && sourceUserCacheExpired(now, seen) && expired < 0:
				expired = i
			case id != 0 && !sourceUserCacheExpired(now, seen):
				age := sourceUserCacheAge(now, seen)
				if oldest < 0 || age > oldestAge {
					oldest = i
					oldestAge = age
				}
			}
		}

		slot := same
		if slot < 0 {
			slot = empty
		}
		if slot < 0 {
			slot = expired
		}
		if slot < 0 {
			slot = oldest
		}
		old := slots[slot]
		oldID, oldTick := sourceUserCacheUnpackUser(old)
		if oldID == userID && oldTick == now {
			return sourceUserCacheRecordTransition{}
		}
		if entry.users[slot].CompareAndSwap(old, sourceUserCachePackUser(userID, now)) {
			oldExpired := oldID != 0 && sourceUserCacheExpired(now, oldTick)
			return sourceUserCacheRecordTransition{
				inserted: oldID == 0 || oldExpired || oldID != userID,
				expired:  oldExpired,
				evicted:  oldID != 0 && !oldExpired && oldID != userID,
			}
		}
		// A failed slot CAS invalidates the selection. Restart with a fresh
		// snapshot so empty, expired, and LRU choices remain deterministic.
	}
}

func (c *sourceUserCache) recordTransition(transition sourceUserCacheRecordTransition) {
	if c.stats == nil {
		return
	}
	if transition.inserted {
		c.stats.insertions.Add(1)
	}
	if transition.expired {
		c.stats.expiries.Add(1)
	}
	if transition.evicted {
		c.stats.evictions.Add(1)
	}
}

func selectSourceUserCacheWay(ways [sourceUserCacheWays]*sourceUserCacheEntry, now uint32) sourceUserCacheWaySelection {
	for way, entry := range ways {
		if entry == nil {
			return sourceUserCacheWaySelection{way: way}
		}
	}

	var ticks [sourceUserCacheWays]uint32
	for way, entry := range ways {
		ticks[way] = entry.lastActive.Load()
		if sourceUserCacheExpired(now, ticks[way]) {
			return sourceUserCacheWaySelection{way: way, entry: entry, expired: true}
		}
	}

	oldest := 0
	oldestAge := sourceUserCacheAge(now, ticks[0])
	for way := 1; way < sourceUserCacheWays; way++ {
		age := sourceUserCacheAge(now, ticks[way])
		if age > oldestAge {
			oldest = way
			oldestAge = age
		}
	}
	return sourceUserCacheWaySelection{way: oldest, entry: ways[oldest]}
}
