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

package replay_test

import (
	crand "crypto/rand"
	"testing"
	"time"

	"github.com/enfein/mieru/pkg/replay"
)

func TestDuplicate(t *testing.T) {
	cache := replay.NewCache(10, 1*time.Minute)
	data := make([]byte, 256)
	if _, err := crand.Read(data); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}

	if res := cache.IsDuplicate(data, replay.EmptyTag); res == true {
		t.Errorf("IsDuplicate() = true, want false")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 1 0.", curr, prev)
	}

	if res := cache.IsDuplicate(data, replay.EmptyTag); res == false {
		t.Errorf("IsDuplicate() = false, want true")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 1 0.", curr, prev)
	}
}

func TestNoDuplicate(t *testing.T) {
	cache := replay.NewCache(10, 1*time.Minute)
	a := make([]byte, 1)
	if _, err := crand.Read(a); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}
	b := make([]byte, 31)
	if _, err := crand.Read(b); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}

	if res := cache.IsDuplicate(a, replay.EmptyTag); res == true {
		t.Errorf("IsDuplicate() = true, want false")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 1 0.", curr, prev)
	}

	if res := cache.IsDuplicate(b, replay.EmptyTag); res == true {
		t.Errorf("IsDuplicate() = true, want false")
	}
	if curr, prev := cache.Sizes(); curr != 2 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 2 0.", curr, prev)
	}
}

func TestTag(t *testing.T) {
	cache := replay.NewCache(10, 1*time.Minute)
	data := make([]byte, 32)
	if _, err := crand.Read(data); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}

	if res := cache.IsDuplicate(data, "tag1"); res == true {
		t.Errorf("IsDuplicate() = true, want false")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 1 0.", curr, prev)
	}

	if res := cache.IsDuplicate(data, "tag1"); res == true {
		t.Errorf("IsDuplicate() = true, want false")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 1 0.", curr, prev)
	}

	if res := cache.IsDuplicate(data, "tag2"); res == false {
		t.Errorf("IsDuplicate() = false, want true")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 1 0.", curr, prev)
	}

	if res := cache.IsDuplicate(data, "tag1"); res == true {
		t.Errorf("IsDuplicate() = true, want false")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 1 0.", curr, prev)
	}

	if res := cache.IsDuplicate(data, replay.EmptyTag); res == false {
		t.Errorf("IsDuplicate() = false, want true")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 1 0.", curr, prev)
	}
}

func TestCapacity(t *testing.T) {
	cache := replay.NewCache(1, 1*time.Minute)
	a := make([]byte, 256)
	if _, err := crand.Read(a); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}
	b := make([]byte, 256)
	if _, err := crand.Read(b); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}

	if res := cache.IsDuplicate(a, replay.EmptyTag); res == true {
		t.Errorf("IsDuplicate() = true, want false")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 1 0.", curr, prev)
	}

	if res := cache.IsDuplicate(a, replay.EmptyTag); res == false {
		t.Errorf("IsDuplicate() = false, want true")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 1 {
		t.Errorf("cache sizes are %d %d, want 1 1.", curr, prev)
	}

	if res := cache.IsDuplicate(b, replay.EmptyTag); res == true {
		t.Errorf("IsDuplicate() = true, want false")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 1 {
		t.Errorf("cache sizes are %d %d, want 1 1.", curr, prev)
	}

	cache.Clear()
	if curr, prev := cache.Sizes(); curr != 0 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 0 0.", curr, prev)
	}
}

func TestExpireInterval(t *testing.T) {
	cache := replay.NewCache(10, 50*time.Millisecond)
	a := make([]byte, 256)
	if _, err := crand.Read(a); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}
	b := make([]byte, 256)
	if _, err := crand.Read(b); err != nil {
		t.Fatalf("rand.Read() failed: %v", err)
	}

	if res := cache.IsDuplicate(a, replay.EmptyTag); res == true {
		t.Errorf("IsDuplicate() = true, want false")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 0 {
		t.Errorf("cache sizes are %d %d, want 1 0.", curr, prev)
	}

	time.Sleep(100 * time.Millisecond)

	if res := cache.IsDuplicate(a, replay.EmptyTag); res == false {
		t.Errorf("IsDuplicate() = false, want true")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 1 {
		t.Errorf("cache sizes are %d %d, want 1 1.", curr, prev)
	}

	time.Sleep(100 * time.Millisecond)

	if res := cache.IsDuplicate(b, replay.EmptyTag); res == true {
		t.Errorf("IsDuplicate() = true, want false")
	}
	if curr, prev := cache.Sizes(); curr != 1 || prev != 1 {
		t.Errorf("cache sizes are %d %d, want 1 1.", curr, prev)
	}
}
