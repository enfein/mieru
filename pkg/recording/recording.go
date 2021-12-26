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

package recording

import (
	"sync"
	"time"
)

type TrafficDirection int

const (
	Ingress TrafficDirection = 0
	Egress  TrafficDirection = 1
)

// Record contains a single recorded packet.
type Record struct {
	timestamp time.Time
	data      []byte
	direction TrafficDirection
}

func NewRecord(data []byte, direction TrafficDirection) Record {
	d := make([]byte, len(data))
	if len(data) > 0 {
		copy(d, data)
	}
	return Record{
		timestamp: time.Now(),
		data:      d,
		direction: direction,
	}
}

func (r *Record) Timestamp() time.Time {
	return r.timestamp
}

func (r *Record) Data() []byte {
	return r.data
}

func (r *Record) Direction() TrafficDirection {
	return r.direction
}

// Records contains a series of recorded packets.
type Records struct {
	mu    sync.Mutex
	table []Record
}

func NewRecords() Records {
	return Records{
		table: make([]Record, 0),
	}
}

func (r *Records) Append(data []byte, direction TrafficDirection) {
	record := NewRecord(data, direction)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.table = append(r.table, record)
}

func (r *Records) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.table)
}

func (r *Records) Export() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	records := make([]Record, len(r.table))
	copy(records, r.table)
	return records
}

func (r *Records) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.table = make([]Record, 0)
}
