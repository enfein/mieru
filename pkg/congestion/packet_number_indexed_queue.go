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
	"sync"

	"github.com/enfein/mieru/v3/pkg/deque"
)

// PacketNumberIndexedQueue is a queue of mostly continuous numbered entries
// which supports the following operations:
// - adding elements to the end of the queue, or at some point past the end
// - removing elements in any order
// - retrieving elements
// If all elements are inserted in order, all of the operations above are
// amortized O(1) time.
//
// Internally, the data structure is a deque where each element is marked as
// present or not.  The deque starts at the lowest present index. Whenever an
// element is removed, it's marked as not present, and the front of the deque is
// cleared of elements that are not present.
//
// The tail of the queue is not cleared due to the assumption of entries being
// inserted in order, though removing all elements of the queue will return it
// to its initial state.
//
// Note that this data structure is inherently hazardous, since an addition of
// just two entries will cause it to consume all of the memory available.
// Because of that, it is not a general-purpose container and should not be used
// as one.
type PacketNumberIndexedQueue[T any] struct {
	mu                     sync.Mutex
	entries                *deque.Deque[EntryWrapper[T]]
	numberOfPresentEntries int
	firstPacket            int64
}

func NewPacketNumberIndexedQueue[T any]() *PacketNumberIndexedQueue[T] {
	return &PacketNumberIndexedQueue[T]{
		entries:                deque.New[EntryWrapper[T]](0),
		numberOfPresentEntries: 0,
		firstPacket:            -1,
	}
}

// EntryWrapper is an element marked as present or not.
type EntryWrapper[T any] struct {
	data    T
	present bool
}

// GetEntry retrieves the entry associated with the packet number.
// Returns the pointer to the entry in case of success, or nil if the entry
// does not exist.
func (p *PacketNumberIndexedQueue[T]) GetEntry(packetNumber int64) *T {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, idx := p.getEntryWrapper(packetNumber)
	if idx < 0 {
		return nil
	}
	return &entry.data
}

// Emplace inserts data associated packetNumber into (or past) the end of the
// queue, filling up the missing intermediate entries as necessary. Returns
// true if the element has been inserted successfully, false if it was already
// in the queue or inserted out of order.
func (p *PacketNumberIndexedQueue[T]) Emplace(packetNumber int64, args T) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.numberOfPresentEntries == 0 {
		p.entries.PushBack(EntryWrapper[T]{data: args, present: true})
		p.numberOfPresentEntries = 1
		p.firstPacket = packetNumber
		return true
	}

	// Do not allow insertion out-of-order.
	if packetNumber <= p.LastPacket() {
		return false
	}

	// Handle potentially missing elements.
	offset := packetNumber - p.firstPacket
	for int64(p.entries.Len()) < offset {
		p.entries.PushBack(EntryWrapper[T]{})
	}

	p.numberOfPresentEntries++
	p.entries.PushBack(EntryWrapper[T]{data: args, present: true})
	return packetNumber == p.LastPacket()
}

// Remove removes data associated with packetNumber and frees the slots in the
// queue as necessary.
func (p *PacketNumberIndexedQueue[T]) Remove(packetNumber int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, idx := p.getEntryWrapper(packetNumber)
	if idx < 0 {
		return false
	}
	entry.present = false
	p.entries.Set(idx, entry)
	p.numberOfPresentEntries--

	if packetNumber == p.firstPacket {
		p.cleanup()
	}
	return true
}

func (p *PacketNumberIndexedQueue[T]) IsEmpty() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.numberOfPresentEntries == 0
}

func (p *PacketNumberIndexedQueue[T]) FirstPacket() int64 {
	return p.firstPacket
}

func (p *PacketNumberIndexedQueue[T]) LastPacket() int64 {
	if p.numberOfPresentEntries == 0 {
		return -1
	}
	return p.firstPacket + int64(p.entries.Len()) - 1
}

func (p *PacketNumberIndexedQueue[T]) entrySlotsUsed() int {
	return p.entries.Len()
}

// cleanup cleans up unused slots in the front.
func (p *PacketNumberIndexedQueue[T]) cleanup() {
	for p.entries.Len() > 0 && !p.entries.Front().present {
		p.entries.PopFront()
		p.firstPacket++
	}
	if p.entries.Len() == 0 {
		p.firstPacket = -1
	}
}

// getEntryWrapper returns a copy of the wrapper and the index in the deque.
// If not found, index -1 is returned.
func (p *PacketNumberIndexedQueue[T]) getEntryWrapper(packetNumber int64) (EntryWrapper[T], int) {
	if packetNumber < p.firstPacket {
		return EntryWrapper[T]{}, -1
	}

	offset := packetNumber - p.firstPacket
	if offset >= int64(p.entries.Len()) {
		return EntryWrapper[T]{}, -1
	}

	entry := p.entries.At(int(offset))
	if !entry.present {
		return EntryWrapper[T]{}, -1
	}

	return entry, int(offset)
}
