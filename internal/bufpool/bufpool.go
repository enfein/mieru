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

// Package bufpool reuses byte storage shared across operations. Prefer ordinary
// slices or fixed arrays when storage already has a local lifetime.
//
// Each buffer has one owner, which must release it exactly once after the last
// access to its bytes. Ownership may be transferred, but a buffer and its bytes
// must not be accessed concurrently or after release. Storage is not cleared on
// reuse: callers must initialize every byte they transmit.
//
// The pools may discard entries during garbage collection and do not impose a
// hard limit on total cached memory. Requests larger than the largest size class
// use ordinary allocations, which are discarded on release.
package bufpool

import "sync"

var classes = [...]struct {
	size int
	pool sync.Pool
}{
	{size: 256},            // Small metadata and padding.
	{size: 2 * 1024},       // MTU-sized packets with framing overhead.
	{size: 8*1024 + 1024},  // Intermediate packets with framing overhead.
	{size: 32*1024 + 1024}, // Stream chunks plus metadata, tags, and padding.
	{size: 64*1024 + 1024}, // Includes the maximum 65,539-byte (65535 + 4) PacketOverStreamTunnel frame.
}

// Buffer owns an allocation independently of any subslices returned by Bytes.
// Obtain a *Buffer with Acquire; do not copy it or use its zero value.
type Buffer struct {
	storage []byte
	size    int
	pool    *sync.Pool
}

// Acquire returns an owner of size bytes. It panics if size is negative.
// If the requested size is too big, the buffer won't be added to the pool.
func Acquire(size int) *Buffer {
	if size < 0 {
		panic("bufpool: negative size")
	}
	for i := range classes {
		class := &classes[i]
		if size > class.size {
			continue
		}
		if cached := class.pool.Get(); cached != nil {
			buf := cached.(*Buffer)
			buf.size = size
			return buf
		}
		return &Buffer{storage: make([]byte, class.size), size: size, pool: &class.pool}
	}
	return &Buffer{storage: make([]byte, size), size: size}
}

// Bytes returns the requested bytes with length and capacity equal to the size
// passed to Acquire. Reslicing the returned view does not affect ownership.
// The view and all its subslices are valid only until Release is called.
func (b *Buffer) Bytes() []byte {
	return b.storage[:b.size:b.size]
}

// Release returns the owner and its original allocation to their size class,
// or discards oversized storage. Call it exactly once, after all users of the
// buffer and its views have finished. The owner must not reuse it afterward.
func (b *Buffer) Release() {
	b.size = 0
	if b.pool != nil {
		b.pool.Put(b)
	} else {
		b.storage = nil
	}
}
