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

package protocol

import (
	"encoding/binary"
	"math"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

var (
	sourceUserCacheBenchCandidates [sourceUserCacheUsers]uint32
	sourceUserCacheBenchCount      int
)

type sourceUserCacheTestClock struct {
	tick atomic.Uint32
}

func (c *sourceUserCacheTestClock) now() uint32 {
	return c.tick.Load()
}

func (c *sourceUserCacheTestClock) set(tick uint32) {
	c.tick.Store(tick)
}

type unsupportedSourceUserCacheAddr struct{}

func (unsupportedSourceUserCacheAddr) Network() string { return "unsupported" }
func (unsupportedSourceUserCacheAddr) String() string  { return "192.0.2.1:1234" }

func TestSourceUserCacheAddressNormalization(t *testing.T) {
	ipv4TCP, ok := sourceUserCacheKey(&net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1000})
	if !ok {
		t.Fatal("IPv4 TCP address was rejected")
	}
	ipv4UDP, ok := sourceUserCacheKey(&net.UDPAddr{IP: net.ParseIP("::ffff:192.0.2.1"), Port: 2000})
	if !ok {
		t.Fatal("IPv4-mapped IPv6 UDP address was rejected")
	}
	if ipv4TCP != ipv4UDP {
		t.Fatalf("IPv4 key %x differs from mapped IPv6 key %x", ipv4TCP, ipv4UDP)
	}

	ipv6TCP, ok := sourceUserCacheKey(&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 3000, Zone: "eth0"})
	if !ok {
		t.Fatal("IPv6 TCP address was rejected")
	}
	ipv6UDP, ok := sourceUserCacheKey(&net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 4000, Zone: "eth1"})
	if !ok {
		t.Fatal("IPv6 UDP address was rejected")
	}
	if ipv6TCP != ipv6UDP {
		t.Fatalf("IPv6 key changed with source port or zone: %x != %x", ipv6TCP, ipv6UDP)
	}
	if ipv4TCP == ipv6TCP {
		t.Fatal("distinct IPv4 and IPv6 addresses have the same full key")
	}
}

func TestSourceUserCacheAddressNormalizationRejectsInvalid(t *testing.T) {
	var nilTCP *net.TCPAddr
	var nilUDP *net.UDPAddr
	for _, addr := range []net.Addr{
		nil,
		nilTCP,
		nilUDP,
		&net.TCPAddr{IP: net.IP{1, 2, 3}, Port: 1000},
		&net.UDPAddr{Port: 1000},
		&net.IPAddr{IP: net.IPv4(192, 0, 2, 1)},
		unsupportedSourceUserCacheAddr{},
	} {
		if key, ok := sourceUserCacheKey(addr); ok {
			t.Errorf("sourceUserCacheKey(%T) = (%x, true), want rejection", addr, key)
		}
	}
}

func TestSourceUserCacheExactKeyValidationUnderBucketCollision(t *testing.T) {
	clock := &sourceUserCacheTestClock{}
	clock.set(100)
	cache := newSourceUserCacheWithTick(&sourceUserCacheStats{}, clock.now)
	keys := sourceUserCacheCollidingKeys(2)
	cache.recordAuthenticated(keys[0], 7)

	if candidates, count := cache.lookup(keys[0]); count != 1 || candidates[0] != 7 {
		t.Fatalf("recorded key lookup = (%v, %d), want ([7], 1)", candidates, count)
	}
	if candidates, count := cache.lookup(keys[1]); count != 0 {
		t.Fatalf("colliding key lookup = (%v, %d), want miss", candidates, count)
	}
}

func TestSourceUserCacheMRUDedupExpiryAndUserEviction(t *testing.T) {
	clock := &sourceUserCacheTestClock{}
	cache := newSourceUserCacheWithTick(&sourceUserCacheStats{}, clock.now)
	key := sourceUserCacheTestKey(1)
	for userID := uint32(1); userID <= sourceUserCacheUsers; userID++ {
		clock.set(1000 + userID)
		cache.recordAuthenticated(key, userID)
	}

	clock.set(1020)
	cache.recordAuthenticated(key, 3)
	clock.set(1021)
	cache.recordAuthenticated(key, 11)
	candidates, count := cache.lookup(key)
	if count != sourceUserCacheUsers || candidates[0] != 11 || candidates[1] != 3 {
		t.Fatalf("MRU candidates = (%v, %d), want users 11 and 3 first", candidates, count)
	}
	if sourceUserCacheContains(candidates, count, 1) {
		t.Fatalf("oldest user 1 was not evicted: %v", candidates)
	}

	entry := sourceUserCacheEntryForKey(cache.loadTable(), key)
	if entry == nil {
		t.Fatal("source entry disappeared")
	}
	// Inject a duplicate with an older timestamp. Lookup must return the ID
	// once and retain the newer position.
	entry.users[1].Store(sourceUserCachePackUser(3, 1010))
	candidates, count = cache.lookup(key)
	if count != sourceUserCacheUsers-1 || candidates[0] != 11 || candidates[1] != 3 {
		t.Fatalf("deduplicated candidates = (%v, %d), want 9 users with 11 and 3 first", candidates, count)
	}

	clock.set(1621)
	if candidates, count = cache.lookup(key); count != 0 {
		t.Fatalf("expired lookup = (%v, %d), want miss", candidates, count)
	}
}

func TestSourceUserCacheReusesExpiredUserBeforeLiveUser(t *testing.T) {
	clock := &sourceUserCacheTestClock{}
	clock.set(1000)
	cache := newSourceUserCacheWithTick(nil, clock.now)
	key := sourceUserCacheTestKey(2)
	cache.recordAuthenticated(key, 1)
	for userID := uint32(2); userID <= sourceUserCacheUsers; userID++ {
		clock.set(1590 + userID)
		cache.recordAuthenticated(key, userID)
	}
	clock.set(1601)
	cache.recordAuthenticated(key, 11)
	candidates, count := cache.lookup(key)
	if count != sourceUserCacheUsers || sourceUserCacheContains(candidates, count, 1) {
		t.Fatalf("expired user was not reused before a live user: (%v, %d)", candidates, count)
	}
	for userID := uint32(2); userID <= 11; userID++ {
		if !sourceUserCacheContains(candidates, count, userID) {
			t.Fatalf("live user %d was evicted: %v", userID, candidates)
		}
	}
}

func TestSourceUserCacheFourWayReplacementAndMRUPromotion(t *testing.T) {
	clock := &sourceUserCacheTestClock{}
	cache := newSourceUserCacheWithTick(nil, clock.now)
	keys := sourceUserCacheCollidingKeys(sourceUserCacheWays + 1)
	for i := 0; i < sourceUserCacheWays; i++ {
		clock.set(uint32(100 + i))
		cache.recordAuthenticated(keys[i], uint32(i+1))
	}

	clock.set(104)
	cache.recordAuthenticated(keys[0], 1)
	clock.set(105)
	cache.recordAuthenticated(keys[4], 5)
	if _, count := cache.lookup(keys[1]); count != 0 {
		t.Fatal("least-recently-used source was not replaced")
	}
	for _, index := range []int{0, 2, 3, 4} {
		if _, count := cache.lookup(keys[index]); count != 1 {
			t.Fatalf("retained source %d missed", index)
		}
	}
}

func TestSourceUserCacheConcurrentlyNewerTickMayMiss(t *testing.T) {
	if age := sourceUserCacheAge(1000, 1001); age != math.MaxUint32 {
		t.Fatalf("age(1000, 1001) = %d, want %d", age, uint32(math.MaxUint32))
	}
	if !sourceUserCacheExpired(1000, 1001) {
		t.Fatal("a tick newer than the lookup snapshot should produce a safe transient miss")
	}

	clock := &sourceUserCacheTestClock{}
	clock.set(1001)
	cache := newSourceUserCacheWithTick(nil, clock.now)
	key := sourceUserCacheTestKey(6)
	cache.recordAuthenticated(key, 1)
	clock.set(1000)
	if candidates, count := cache.lookup(key); count != 0 {
		t.Fatalf("lookup with concurrently newer tick = (%v, %d), want a safe miss", candidates, count)
	}
}

func TestSourceUserCacheExpiredEntryDoesNotResurrectAfterHalfRange(t *testing.T) {
	clock := &sourceUserCacheTestClock{}
	clock.set(100)
	cache := newSourceUserCacheWithTick(nil, clock.now)
	key := sourceUserCacheTestKey(7)
	cache.recordAuthenticated(key, 1)

	clock.set(uint32(100 + (1 << 31) + 1))
	if candidates, count := cache.lookup(key); count != 0 {
		t.Fatalf("expired source resurrected after half the tick range: (%v, %d)", candidates, count)
	}

	// Keep the source entry current to verify that its old user slot also
	// remains expired rather than being interpreted as a future timestamp.
	entry := sourceUserCacheEntryForKey(cache.loadTable(), key)
	if entry == nil {
		t.Fatal("source entry disappeared")
	}
	entry.lastActive.Store(clock.now())
	if candidates, count := cache.lookup(key); count != 0 {
		t.Fatalf("expired user resurrected after half the tick range: (%v, %d)", candidates, count)
	}

	cache.recordAuthenticated(key, 1)
	if candidates, count := cache.lookup(key); count != 1 || candidates[0] != 1 {
		t.Fatalf("authenticated refresh after half the tick range = (%v, %d), want ([1], 1)", candidates, count)
	}
}

func TestSourceUserCacheConcurrentFirstInsertionConverges(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run("full="+strconv.FormatBool(full), func(t *testing.T) {
			clock := &sourceUserCacheTestClock{}
			clock.set(100)
			cache := newSourceUserCacheWithTick(nil, clock.now)
			key := sourceUserCacheTestKey(3)
			if full {
				keys := sourceUserCacheCollidingKeys(sourceUserCacheWays + 1)
				for i := 0; i < sourceUserCacheWays; i++ {
					cache.recordAuthenticated(keys[i], uint32(i+1))
					clock.set(clock.now() + 1)
				}
				key = keys[sourceUserCacheWays]
			}

			const goroutines = 64
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := 0; i < goroutines; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					cache.recordAuthenticated(key, 1)
				}()
			}
			close(start)
			wg.Wait()

			bucket := &cache.loadTable().buckets[sourceUserCacheBucketIndex(key)]
			matches := 0
			for i := 0; i < sourceUserCacheWays; i++ {
				entry := bucket.ways[i].Load()
				if entry != nil && entry.key == key {
					matches++
				}
			}
			if matches != 1 {
				t.Fatalf("persistent entries for one key = %d, want 1", matches)
			}
		})
	}
}

func TestSourceUserCacheConcurrentRefreshAndReplacement(t *testing.T) {
	clock := &sourceUserCacheTestClock{}
	clock.set(200)
	cache := newSourceUserCacheWithTick(nil, clock.now)
	keys := sourceUserCacheCollidingKeys(sourceUserCacheWays + 1)
	bucket := &cache.loadTable().buckets[sourceUserCacheBucketIndex(keys[0])]

	for attempt := 0; attempt < 200; attempt++ {
		for way := 0; way < sourceUserCacheWays; way++ {
			entry := &sourceUserCacheEntry{key: keys[way]}
			entry.lastActive.Store(uint32(100 + way))
			entry.users[0].Store(sourceUserCachePackUser(uint32(way+1), uint32(100+way)))
			bucket.ways[way].Store(entry)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			cache.recordAuthenticated(keys[0], 10)
		}()
		go func() {
			defer wg.Done()
			<-start
			cache.recordAuthenticated(keys[sourceUserCacheWays], 5)
		}()
		close(start)
		wg.Wait()

		for _, index := range []int{0, 2, 3, 4} {
			if _, count := cache.lookup(keys[index]); count == 0 {
				t.Fatalf("attempt %d: source %d was lost during concurrent refresh and replacement", attempt, index)
			}
		}
		if _, count := cache.lookup(keys[1]); count != 0 {
			t.Fatalf("attempt %d: next-oldest source was not replaced", attempt)
		}
	}
}

func TestSourceUserCacheConcurrentOperationsAndRetirement(t *testing.T) {
	clock := &sourceUserCacheTestClock{}
	clock.set(100)
	stats := &sourceUserCacheStats{}
	cache := newSourceUserCacheWithTick(stats, clock.now)
	keys := sourceUserCacheCollidingKeys(16)
	var wg sync.WaitGroup
	start := make(chan struct{})
	started := make(chan struct{}, 8)
	var stop atomic.Bool
	for worker := 0; worker < 8; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			i := 0
			for !stop.Load() {
				key := keys[(worker+i)%len(keys)]
				cache.recordAuthenticated(key, uint32(worker+1))
				cache.lookup(key)
				if i == 0 {
					started <- struct{}{}
				}
				i++
			}
		}()
	}
	close(start)
	for i := 0; i < 8; i++ {
		<-started
	}
	cache.retire()
	stop.Store(true)
	wg.Wait()

	records := stats.records.Load()
	cache.recordAuthenticated(keys[0], 99)
	if stats.records.Load() != records {
		t.Fatal("record started after retirement modified the detached table")
	}
	if candidates, count := cache.lookup(keys[0]); count != 0 {
		t.Fatalf("retired cache lookup = (%v, %d), want miss", candidates, count)
	}
}

func TestSourceUserCacheTickWraparound(t *testing.T) {
	clock := &sourceUserCacheTestClock{}
	cache := newSourceUserCacheWithTick(nil, clock.now)
	key := sourceUserCacheTestKey(4)
	clock.set(math.MaxUint32 - 5)
	cache.recordAuthenticated(key, 1)
	clock.set(math.MaxUint32 - 2)
	cache.recordAuthenticated(key, 2)
	clock.set(2)
	cache.recordAuthenticated(key, 3)

	candidates, count := cache.lookup(key)
	if count != 3 || candidates[0] != 3 || candidates[1] != 2 || candidates[2] != 1 {
		t.Fatalf("wraparound MRU lookup = (%v, %d), want [3 2 1]", candidates, count)
	}
	clock.set(602)
	if candidates, count = cache.lookup(key); count != 0 {
		t.Fatalf("wraparound expired lookup = (%v, %d), want miss", candidates, count)
	}
}

func TestSourceUserCacheStatsAndZeroAllocationWarmPath(t *testing.T) {
	clock := &sourceUserCacheTestClock{}
	clock.set(100)
	stats := &sourceUserCacheStats{}
	cache := newSourceUserCacheWithTick(stats, clock.now)
	key := sourceUserCacheTestKey(5)
	if _, count := cache.lookup(key); count != 0 {
		t.Fatal("empty cache lookup unexpectedly hit")
	}
	cache.recordAuthenticated(key, 1)
	if _, count := cache.lookup(key); count != 1 {
		t.Fatal("recorded cache lookup missed")
	}
	if stats.lookups.Load() != 2 || stats.hits.Load() != 1 || stats.misses.Load() != 1 || stats.records.Load() != 1 {
		t.Fatalf("unexpected stats: lookups=%d hits=%d misses=%d records=%d", stats.lookups.Load(), stats.hits.Load(), stats.misses.Load(), stats.records.Load())
	}

	lookupAllocs := testing.AllocsPerRun(1000, func() {
		sourceUserCacheBenchCandidates, sourceUserCacheBenchCount = cache.lookup(key)
	})
	if lookupAllocs != 0 {
		t.Fatalf("warm lookup allocations = %f, want 0", lookupAllocs)
	}
	recordAllocs := testing.AllocsPerRun(1000, func() {
		cache.recordAuthenticated(key, 1)
	})
	if recordAllocs != 0 {
		t.Fatalf("warm refresh allocations = %f, want 0", recordAllocs)
	}
}

func TestSourceUserCachePhysicalLimitAndUniformOccupancy(t *testing.T) {
	if slots := sourceUserCacheBucketCount * sourceUserCacheWays; slots != 65536 {
		t.Fatalf("physical source slots = %d, want 65536", slots)
	}
	wantTableSize := uintptr(sourceUserCacheBucketCount*sourceUserCacheWays) * unsafe.Sizeof(atomic.Pointer[sourceUserCacheEntry]{})
	if tableSize := unsafe.Sizeof(sourceUserCacheTable{}); tableSize != wantTableSize {
		t.Fatalf("source table = %d bytes, want pointer-only layout of %d bytes", tableSize, wantTableSize)
	}
	if unsafe.Offsetof(sourceUserCacheEntry{}.users)%unsafe.Alignof(atomic.Uint64{}) != 0 {
		t.Fatal("atomic user slots are not naturally aligned")
	}

	clock := &sourceUserCacheTestClock{}
	clock.set(100)
	cache := newSourceUserCacheWithTick(nil, clock.now)
	const sourceCount = sourceUserCacheBucketCount * sourceUserCacheWays
	for i := 0; i < sourceCount; i++ {
		cache.recordAuthenticated(sourceUserCacheTestKey(uint64(i)), 1)
	}
	occupied := 0
	table := cache.loadTable()
	for bucket := 0; bucket < sourceUserCacheBucketCount; bucket++ {
		for way := 0; way < sourceUserCacheWays; way++ {
			if table.buckets[bucket].ways[way].Load() != nil {
				occupied++
			}
		}
	}
	if occupied >= sourceCount {
		t.Fatalf("uniform occupancy = %d, want lower than physical limit due to bucket collisions", occupied)
	}
	if occupied < 48000 {
		t.Fatalf("uniform occupancy = %d, want at least 48000", occupied)
	}
	t.Logf("uniform occupancy = %d/%d; table = %d bytes; entry = %d bytes", occupied, sourceCount, unsafe.Sizeof(sourceUserCacheTable{}), unsafe.Sizeof(sourceUserCacheEntry{}))
}

func BenchmarkSourceUserCache(b *testing.B) {
	for _, users := range []int{1, sourceUserCacheUsers} {
		b.Run("LookupCandidates_"+strconv.Itoa(users), func(b *testing.B) {
			clock := &sourceUserCacheTestClock{}
			clock.set(100)
			cache := newSourceUserCacheWithTick(&sourceUserCacheStats{}, clock.now)
			key := sourceUserCacheTestKey(10)
			for userID := 1; userID <= users; userID++ {
				cache.recordAuthenticated(key, uint32(userID))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sourceUserCacheBenchCandidates, sourceUserCacheBenchCount = cache.lookup(key)
			}
		})
	}
	b.Run("RefreshParallel", func(b *testing.B) {
		clock := &sourceUserCacheTestClock{}
		clock.set(100)
		cache := newSourceUserCacheWithTick(&sourceUserCacheStats{}, clock.now)
		key := sourceUserCacheTestKey(11)
		cache.recordAuthenticated(key, 1)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				cache.recordAuthenticated(key, 1)
			}
		})
	})
}

func sourceUserCacheTestKey(value uint64) [16]byte {
	var key [16]byte
	key[0] = 0x20
	key[1] = 0x01
	key[2] = 0x0d
	key[3] = 0xb8
	binary.BigEndian.PutUint64(key[8:], value)
	return key
}

func sourceUserCacheCollidingKeys(count int) [][16]byte {
	keys := make([][16]byte, 0, count)
	target := sourceUserCacheBucketIndex(sourceUserCacheTestKey(0))
	for value := uint64(0); len(keys) < count; value++ {
		key := sourceUserCacheTestKey(value)
		if sourceUserCacheBucketIndex(key) == target {
			keys = append(keys, key)
		}
	}
	return keys
}

func sourceUserCacheEntryForKey(table *sourceUserCacheTable, key [16]byte) *sourceUserCacheEntry {
	bucket := &table.buckets[sourceUserCacheBucketIndex(key)]
	for i := 0; i < sourceUserCacheWays; i++ {
		entry := bucket.ways[i].Load()
		if entry != nil && entry.key == key {
			return entry
		}
	}
	return nil
}

func sourceUserCacheContains(candidates [sourceUserCacheUsers]uint32, count int, userID uint32) bool {
	for i := 0; i < count; i++ {
		if candidates[i] == userID {
			return true
		}
	}
	return false
}
