// Copyright (C) 2022  mieru authors
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

package slicepool

import "sync"

// SlicePool is a pool of []byte. Typically *[]byte is preferred to avoid
// heap allocation. However in our case, the slice sizes are different, making
// them not feasible to reuse directly, so we only reuse the underlying array.
type SlicePool struct {
	pool sync.Pool
	len  int // the size of each slice
}

func NewSlicePool(len int) SlicePool {
	return SlicePool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, len)
			},
		},
		len: len,
	}
}

func (p *SlicePool) Get() []byte {
	return p.pool.Get().([]byte)
}

func (p *SlicePool) Put(s []byte) {
	if cap(s) == p.len {
		p.pool.Put(s)
	} else {
		panic("unexpected slice size")
	}
}
