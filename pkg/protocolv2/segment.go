// Copyright (C) 2023  mieru authors
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

package protocolv2

import (
	"sync"

	"github.com/enfein/mieru/pkg/mathext"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/stderror"
	"github.com/google/btree"
)

const (
	// Maximum protocol data unit supported in a single Write() call from application.
	MaxPDU = 16 * 1024
)

// MaxFragmentSize returns the maximum payload size in a fragment.
func MaxFragmentSize(mtu int, ipVersion netutil.IPVersion, transport netutil.TransportProtocol) int {
	if transport == netutil.TCPTransport {
		// No fragment needed.
		return MaxPDU
	}

	res := mtu - udpOverhead
	if ipVersion == netutil.IPVersion4 {
		res -= 20
	} else {
		res -= 40
	}
	if transport == netutil.UDPTransport {
		res -= 8
	} else {
		res -= 20
	}
	return mathext.Max(0, res)
}

// segment contains metadata and actual payload.
type segment struct {
	metadata metadata
	payload  []byte // also can be a fragment
}

// Protocol returns the protocol of the segment.
func (s *segment) Protocol() byte {
	return s.metadata.Protocol()
}

// Seq returns the sequence number of the segment, if possible.
func (s *segment) Seq() (uint32, error) {
	das, ok := s.metadata.(*dataAckStruct)
	if !ok {
		return 0, stderror.ErrUnsupported
	}
	return das.seq, nil
}

// Fragment returns the fragment number of the segment, if possible.
func (s *segment) Fragment() (uint8, error) {
	das, ok := s.metadata.(*dataAckStruct)
	if !ok {
		return 0, stderror.ErrUnsupported
	}
	return das.fragment, nil
}

// Less tests whether the current item is less than the given argument.
func (s *segment) Less(than *segment) bool {
	mySeq, err := s.Seq()
	if err != nil {
		return false
	}
	otherSeq, err := than.Seq()
	if err != nil {
		return false
	}
	return mySeq < otherSeq
}

func segmentLessFunc(a, b *segment) bool {
	return a.Less(b)
}

// segmentTree is a B-tree to store multiple Segment in order.
type segmentTree struct {
	tr    *btree.BTreeG[*segment]
	cap   int
	mu    sync.Mutex
	full  sync.Cond
	empty sync.Cond
}

func newSegmentTree(capacity int) *segmentTree {
	if capacity <= 0 {
		panic("SegmentTree capacity is <= 0")
	}
	st := &segmentTree{
		tr:  btree.NewG(4, segmentLessFunc),
		cap: capacity,
	}
	st.full = *sync.NewCond(&st.mu)
	st.empty = *sync.NewCond(&st.mu)
	return st
}

// Insert adds a new segment to the tree.
func (t *segmentTree) Insert(seg *segment) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tr.Len() >= t.cap {
		return stderror.ErrFull
	}
	t.tr.ReplaceOrInsert(seg)
	t.empty.Broadcast()
	return nil
}

// InsertBlocking is same as Insert, but blocks when the tree is full.
func (t *segmentTree) InsertBlocking(seg *segment) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for t.tr.Len() >= t.cap {
		t.full.Wait()
	}
	t.tr.ReplaceOrInsert(seg)
	t.empty.Broadcast()
}

// DeleteMin removes the smallest item from the tree.
func (t *segmentTree) DeleteMin() (*segment, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tr.Len() == 0 {
		return nil, stderror.ErrEmpty
	}
	seg, _ := t.tr.DeleteMin()
	t.full.Broadcast()
	return seg, nil
}

// DeleteMinBlocking is the same as DeleteMin, but blocks when the tree is empty.
func (t *segmentTree) DeleteMinBlocking() *segment {
	t.mu.Lock()
	defer t.mu.Unlock()

	for t.tr.Len() == 0 {
		t.empty.Wait()
	}
	seg, _ := t.tr.DeleteMin()
	t.full.Broadcast()
	return seg
}

// MinSeq return the minimum sequence number in the SegmentTree.
func (t *segmentTree) MinSeq() (uint32, error) {
	seg, ok := t.tr.Min()
	if !ok {
		return 0, stderror.ErrEmpty
	}
	return seg.Seq()
}

// MinSeq return the maximum sequence number in the SegmentTree.
func (t *segmentTree) MaxSeq() (uint32, error) {
	seg, ok := t.tr.Max()
	if !ok {
		return 0, stderror.ErrEmpty
	}
	return seg.Seq()
}

// Len returns the current size of the tree.
func (t *segmentTree) Len() int {
	return t.tr.Len()
}

// Remaining returns the remaining space of the tree before it is full.
func (t *segmentTree) Remaining() int {
	return t.cap - t.tr.Len()
}
