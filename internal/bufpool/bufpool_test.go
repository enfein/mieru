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

package bufpool

import (
	"bytes"
	"runtime"
	"strconv"
	"testing"
)

func TestAcquire(t *testing.T) {
	tests := []struct {
		size    int
		storage int
		pooled  bool
	}{
		{0, 256, true},
		{1, 256, true},
		{255, 256, true},
		{256, 256, true},
		{257, 2048, true},
		{1400, 2048, true},
		{1500 + 262, 2048, true},
		{2048, 2048, true},
		{2049, 9216, true},
		{8192, 9216, true},
		{8192 + 4, 9216, true},
		{9216, 9216, true},
		{9217, 33792, true},
		{32768, 33792, true},
		{32768 + 4, 33792, true},
		{32768 + 1024, 33792, true},
		{33793, 66560, true},
		{65535 + 4, 66560, true},
		{66560, 66560, true},
		{66561, 66561, false},
		{128 * 1024, 128 * 1024, false},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.size), func(t *testing.T) {
			buf := Acquire(tt.size)
			defer buf.Release()
			data := buf.Bytes()
			if len(data) != tt.size || cap(data) != tt.size {
				t.Fatalf("Bytes() length/capacity = %d/%d, want %d/%d", len(data), cap(data), tt.size, tt.size)
			}
			if len(buf.storage) != tt.storage {
				t.Errorf("storage length = %d, want %d", len(buf.storage), tt.storage)
			}
			if (buf.pool != nil) != tt.pooled {
				t.Errorf("pooled = %v, want %v", buf.pool != nil, tt.pooled)
			}
			for i := range data {
				data[i] = byte(i)
			}
		})
	}
}

func TestAcquireNegativeSize(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Acquire(-1) did not panic")
		}
	}()
	Acquire(-1)
}

func TestSubsliceOwnership(t *testing.T) {
	buf := Acquire(1500)
	defer buf.Release()
	data := buf.Bytes()
	for i := range data {
		data[i] = byte(i)
	}
	payload := data[100:1400]
	for len(payload) > 0 {
		payload = payload[1:]
	}
	if len(buf.Bytes()) != 1500 || &buf.Bytes()[0] != &data[0] {
		t.Fatal("advancing the payload changed the owner's view")
	}

	// Appending beyond the requested size must not overwrite owned storage.
	appended := append(data, 1)
	appended[0] = 99
	if buf.Bytes()[0] != 0 {
		t.Fatal("append beyond the requested size reused owned storage")
	}
}

func TestConcurrentOwners(t *testing.T) {
	for worker := 0; worker < 16; worker++ {
		worker := worker
		t.Run(strconv.Itoa(worker), func(t *testing.T) {
			t.Parallel()
			for iteration := 0; iteration < 100; iteration++ {
				for _, size := range []int{1, 1500, 8192, 32772, 65539, 66561} {
					buf := Acquire(size)
					other := Acquire(size)
					data := buf.Bytes()
					want := bytes.Repeat([]byte{byte(worker + 1)}, size)
					copy(data, want)
					copy(other.Bytes(), bytes.Repeat([]byte{0}, size))
					runtime.Gosched()
					valid := bytes.Equal(data, want)
					other.Release()
					buf.Release()
					if !valid {
						t.Fatalf("buffer of size %d was overwritten by another owner", size)
					}
				}
			}
		})
	}
}

func BenchmarkAcquireRelease(b *testing.B) {
	for _, size := range []int{64, 1500, 9000, 32768, 65539, 66561} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				buf := Acquire(size)
				buf.Bytes()[0] = byte(i)
				buf.Release()
			}
		})
	}
}
