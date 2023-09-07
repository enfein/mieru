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
	"fmt"
	"sync"
	"time"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/mathext"
	"github.com/enfein/mieru/pkg/stderror"
	"github.com/enfein/mieru/pkg/util"
	"github.com/google/btree"
)

const (
	// Maximum protocol data unit supported in a single Write() call from application.
	MaxPDU = 16 * 1024

	// Maxinum number of transmission before marking the session as dead.
	txCountLimit = 20

	// Number of fast ack received before retransmission.
	fastAckLimit = 3

	// Format to print segment TX time.
	segmentTimeFormat = "15:04:05.999"
)

// MaxFragmentSize returns the maximum payload size in a fragment.
func MaxFragmentSize(mtu int, ipVersion util.IPVersion, transport util.TransportProtocol) int {
	if transport == util.TCPTransport {
		// No fragment needed.
		return MaxPDU
	}

	res := mtu - udpOverhead
	if ipVersion == util.IPVersion4 {
		res -= 20
	} else {
		res -= 40
	}
	if transport == util.UDPTransport {
		res -= 8
	} else {
		res -= 20
	}
	return mathext.Max(0, res)
}

// MaxPaddingSize returns the maximum padding size of a segment.
func MaxPaddingSize(mtu int, ipVersion util.IPVersion, transport util.TransportProtocol, fragmentSize int, existingPaddingSize int) int {
	if transport == util.TCPTransport {
		// No limit.
		return 255
	}

	res := mtu - fragmentSize - udpOverhead
	if ipVersion == util.IPVersion4 {
		res -= 20
	} else {
		res -= 40
	}
	if transport == util.UDPTransport {
		res -= 8
	} else {
		res -= 20
	}
	if res <= int(existingPaddingSize) {
		return 0
	}
	return mathext.Min(res-int(existingPaddingSize), 255)
}

// segment contains metadata and actual payload.
type segment struct {
	metadata  metadata
	payload   []byte                 // also can be a fragment
	transport util.TransportProtocol // transport protocol
	txCount   byte                   // number of transmission times
	fastAck   byte                   // accumulated out of order ACK
	txTime    time.Time              // most recent tx time
	txTimeout time.Duration          // need to receive ACK within this duration
	block     cipher.BlockCipher     // cipher block to encrypt or decrypt the payload
}

// Protocol returns the protocol of the segment.
func (s *segment) Protocol() protocolType {
	return s.metadata.Protocol()
}

// SessionID returns the session ID of the segment.
func (s *segment) SessionID() (uint32, error) {
	das, ok := s.metadata.(*dataAckStruct)
	if ok {
		return das.sessionID, nil
	}
	ss, ok := s.metadata.(*sessionStruct)
	if ok {
		return ss.sessionID, nil
	}
	return 0, stderror.ErrUnsupported
}

// Seq returns the sequence number of the segment.
func (s *segment) Seq() (uint32, error) {
	das, ok := s.metadata.(*dataAckStruct)
	if ok {
		return das.seq, nil
	}
	ss, ok := s.metadata.(*sessionStruct)
	if ok {
		return ss.seq, nil
	}
	return 0, stderror.ErrUnsupported
}

// Fragment returns the fragment number of the segment.
func (s *segment) Fragment() uint8 {
	das, ok := s.metadata.(*dataAckStruct)
	if !ok {
		return 0
	}
	return das.fragment
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

func (s *segment) String() string {
	if s.transport == util.UDPTransport && (!util.IsZeroTime(s.txTime) || s.txTimeout != 0) {
		return fmt.Sprintf("segment{metadata=%v, txCount=%v, fastAck=%v, txTime=%v, txTimeout=%v}", s.metadata, s.txCount, s.fastAck, s.txTime.Format(segmentTimeFormat), s.txTimeout)
	}
	return fmt.Sprintf("segment{metadata=%v}", s.metadata)
}

func segmentLessFunc(a, b *segment) bool {
	return a.Less(b)
}

// segmentIterator processes the given segment.
// If it returns false, stop the iteration.
type segmentIterator func(*segment) bool

// segmentTree is a B-tree to store multiple Segment in order.
type segmentTree struct {
	tr     *btree.BTreeG[*segment]
	cap    int
	mu     sync.Mutex
	full   sync.Cond
	empty  sync.Cond
	series int
}

func newSegmentTree(capacity int) *segmentTree {
	if capacity <= 0 {
		panic("segment tree capacity is <= 0")
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
// It returns true if insert is successful.
func (t *segmentTree) Insert(seg *segment) (ok bool) {
	if seg == nil {
		panic("Insert() nil segment")
	}
	if _, err := seg.Seq(); err != nil {
		panic(fmt.Sprintf("%v Seq() failed: %v", seg, err))
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tr.Len() >= t.cap {
		return false
	}
	if seg.Fragment() == 0 {
		t.series++
	}
	prev, replace := t.tr.ReplaceOrInsert(seg)
	if replace {
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v is replaced by %v", seg, prev)
		}
		if prev.Fragment() == 0 {
			t.series--
		}
	} else {
		t.empty.Broadcast()
	}
	return true
}

// InsertBlocking is same as Insert, but blocks when the tree is full.
func (t *segmentTree) InsertBlocking(seg *segment) {
	if seg == nil {
		panic("Insert() nil segment")
	}
	if _, err := seg.Seq(); err != nil {
		panic(fmt.Sprintf("%v Seq() failed: %v", seg, err))
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	for t.tr.Len() >= t.cap {
		t.full.Wait()
	}
	if seg.Fragment() == 0 {
		t.series++
	}
	prev, replace := t.tr.ReplaceOrInsert(seg)
	if replace {
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v is replaced by %v", seg, prev)
		}
		if prev.Fragment() == 0 {
			t.series--
		}
	} else {
		t.empty.Broadcast()
	}
}

// DeleteMin removes the smallest item from the tree.
// It returns true if delete is successful.
func (t *segmentTree) DeleteMin() (*segment, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tr.Len() == 0 {
		return nil, false
	}
	seg, ok := t.tr.DeleteMin()
	if !ok {
		panic("segmentTree.DeleteMin() is called when the tree is empty")
	}
	if seg == nil {
		panic("segmentTree.DeleteMin() return nil")
	}
	if seg.Fragment() == 0 {
		t.series--
	}
	t.full.Broadcast()
	return seg, true
}

// DeleteMinIf removes the smallest item from the tree if
// the input function returns true.
// It returns true if the item is deleted from the tree.
func (t *segmentTree) DeleteMinIf(si segmentIterator) (*segment, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tr.Len() == 0 {
		return nil, false
	}
	seg, ok := t.tr.Min()
	if !ok {
		panic("segmentTree.Min() is called when the tree is empty")
	}
	if seg == nil {
		panic("segmentTree.Min() return nil")
	}
	delete := si(seg)
	if delete {
		seg, ok = t.tr.DeleteMin()
		if !ok {
			panic("segmentTree.DeleteMin() is called when the tree is empty")
		}
		if seg == nil {
			panic("segmentTree.DeleteMin() return nil")
		}
		if seg.Fragment() == 0 {
			t.series--
		}
		t.full.Broadcast()
	}
	return seg, delete
}

// Ascend iterates the segment tree in ascending order,
// until the iterator returns false.
func (t *segmentTree) Ascend(si segmentIterator) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tr.Ascend(btree.ItemIteratorG[*segment](si))
}

// IsReadReady returns true if session is ready to read segments.
func (t *segmentTree) IsReadReady() bool {
	return t.series > 0
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
