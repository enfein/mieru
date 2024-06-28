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
	"testing"
	"time"
)

func TestPacketNumberIndexedQueue(t *testing.T) {
	queue := NewPacketNumberIndexedQueue[int64]()
	total := 10

	// Create and insert packets.
	packets := make([]int64, total)
	for i := 0; i < total; i++ {
		clockTime := time.Now().UnixMilli()
		packets[i] = clockTime
		time.Sleep(10 * time.Millisecond)
	}

	for i := 0; i < total; i++ {
		if ok := queue.Emplace(packets[i], packets[i]); !ok {
			t.Fatalf("Emplace() failed on packet %d", i)
		}
	}
	oldTime := time.Now().Add(-1 * time.Minute).UnixMilli()
	if ok := queue.Emplace(oldTime, oldTime); ok {
		t.Fatalf("Emplace() didn't reject out of order packet")
	}

	for i := 0; i < total; i++ {
		v := queue.GetEntry(packets[i])
		if v == nil {
			t.Fatalf("GetEntry() returned nil")
		}
		if *v != packets[i] {
			t.Fatalf("GetEntry() returned unexpected value")
		}
	}

	if queue.FirstPacket() != packets[0] {
		t.Errorf("Got unexpected first packet %v, want %v", queue.FirstPacket(), packets[0])
	}
	if queue.LastPacket() != packets[total-1] {
		t.Errorf("Got unexpected last packet %v, want %v", queue.LastPacket(), packets[total-1])
	}
	if queue.EntrySlotsUsed() <= total {
		t.Errorf("Queue has %d slots, want more than %d", queue.EntrySlotsUsed(), total)
	}

	for i := total - 1; i >= 0; i-- {
		if ok := queue.Remove(packets[i]); !ok {
			t.Fatalf("Remove() failed on packet %d", i)
		}
	}
	if !queue.IsEmpty() {
		t.Errorf("Queue is not empty after removing all the packets")
	}
}
