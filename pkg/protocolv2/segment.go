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
	// Maximum protocol data unit to write without split to chunks.
	maxPDU = 32 * 1024

	// Maxinum number of transmissions before marking the session as dead.
	txCountLimit = 20

	// Format to print segment TX time.
	segmentTimeFormat = "15:04:05.999"
)

// MaxFragmentSize returns the maximum payload size in a fragment.
func MaxFragmentSize(mtu int, ipVersion util.IPVersion, transport util.TransportProtocol) int {
	if transport == util.TCPTransport {
		// No fragment needed.
		return maxPDU
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
func (s *segment) Less(other *segment) bool {
	mySeq, err := s.Seq()
	if err != nil {
		panic(fmt.Sprintf("%v Seq() failed: %v", s, err))
	}
	otherSeq, err := other.Seq()
	if err != nil {
		panic(fmt.Sprintf("%v Seq() failed: %v", other, err))
	}
	return mySeq < otherSeq
}

func (s *segment) String() string {
	if s.transport == util.UDPTransport && (!util.IsZeroTime(s.txTime) || s.txTimeout != 0) {
		return fmt.Sprintf("segment{metadata=%v, realPayloadLen=%v, txCount=%v, txTime=%v, txTimeout=%v}", s.metadata, len(s.payload), s.txCount, s.txTime.Format(segmentTimeFormat), s.txTimeout)
	}
	return fmt.Sprintf("segment{metadata=%v, realPayloadLen=%v}", s.metadata, len(s.payload))
}

func segmentLessFunc(a, b *segment) bool {
	return a.Less(b)
}

// segmentIterator processes the given segment.
// If it returns false, stop the iteration.
type segmentIterator func(*segment) bool

// segmentTree is a B-tree to store multiple Segment in order.
type segmentTree struct {
	tr  *btree.BTreeG[*segment]
	cap int

	mu                sync.Mutex
	notFull           sync.Cond
	chanNotEmptyEvent chan struct{}
}

func newSegmentTree(capacity int) *segmentTree {
	if capacity <= 0 {
		panic("segment tree capacity is <= 0")
	}
	st := &segmentTree{
		tr:  btree.NewG(4, segmentLessFunc),
		cap: capacity,
	}
	st.notFull = *sync.NewCond(&st.mu)
	st.chanNotEmptyEvent = make(chan struct{}, 1)
	return st
}

// Insert adds a new segment to the tree.
// It returns true if insert is successful.
func (t *segmentTree) Insert(seg *segment) (ok bool) {
	t.checkNil(seg)
	t.checkSeq(seg)
	t.checkProtocolType(seg)
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tr.Len() >= t.cap {
		t.notifyNotEmpty()
		return false
	}
	prev, replace := t.tr.ReplaceOrInsert(seg)
	if replace {
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v is replaced by %v", prev, seg)
		}
	} else {
		t.notifyNotEmpty()
	}
	return true
}

// InsertBlocking is same as Insert, but blocks when the tree is full.
func (t *segmentTree) InsertBlocking(seg *segment) {
	t.checkNil(seg)
	t.checkSeq(seg)
	t.checkProtocolType(seg)
	t.mu.Lock()
	defer t.mu.Unlock()

	for t.tr.Len() >= t.cap {
		t.notifyNotEmpty()
		t.notFull.Wait()
	}
	prev, replace := t.tr.ReplaceOrInsert(seg)
	if replace {
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("%v is replaced by %v", prev, seg)
		}
	} else {
		t.notifyNotEmpty()
	}
}

// DeleteMin removes the smallest item from the tree.
// It returns true if delete is successful.
func (t *segmentTree) DeleteMin() (*segment, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Len() == 0 {
		return nil, false
	}
	seg, ok := t.tr.DeleteMin()
	if !ok {
		panic("segmentTree.DeleteMin() is called when the tree is empty")
	}
	if seg == nil {
		panic("segmentTree.DeleteMin() return nil")
	}
	t.notFull.Broadcast()
	if t.Len() > 0 {
		t.notifyNotEmpty()
	}
	return seg, true
}

// DeleteMinIf removes the smallest item from the tree if
// the input function returns true.
// It returns true if the item is deleted from the tree.
func (t *segmentTree) DeleteMinIf(si segmentIterator) (*segment, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.Len() == 0 {
		return nil, false
	}
	seg, ok := t.tr.Min()
	if !ok {
		panic("segmentTree.Min() is called when the tree is empty")
	}
	if seg == nil {
		panic("segmentTree.Min() return nil")
	}
	shouldDelete := si(seg)
	if shouldDelete {
		seg, ok = t.tr.DeleteMin()
		if !ok {
			panic("segmentTree.DeleteMin() is called when the tree is empty")
		}
		if seg == nil {
			panic("segmentTree.DeleteMin() return nil")
		}
		t.notFull.Broadcast()
	}
	if t.Len() > 0 {
		t.notifyNotEmpty()
	}
	return seg, shouldDelete
}

// Ascend iterates the segment tree in ascending order, until the iterator returns false.
// Segments shouldn't be inserted or deleted during the iteration.
func (t *segmentTree) Ascend(si segmentIterator) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tr.Ascend(btree.ItemIteratorG[*segment](si))
}

// MinSeq returns the minimum sequence number in the SegmentTree.
func (t *segmentTree) MinSeq() (uint32, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	seg, ok := t.tr.Min()
	if !ok {
		return 0, stderror.ErrEmpty
	}
	return seg.Seq()
}

// MinSeq returns the maximum sequence number in the SegmentTree.
func (t *segmentTree) MaxSeq() (uint32, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
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

func (t *segmentTree) notifyNotEmpty() {
	select {
	case t.chanNotEmptyEvent <- struct{}{}:
	default:
	}
}

func (t *segmentTree) checkNil(seg *segment) {
	if seg == nil {
		panic("Inserting nil segment to the segment tree")
	}
}

func (t *segmentTree) checkSeq(seg *segment) {
	if _, err := seg.Seq(); err != nil {
		panic(fmt.Sprintf("Failed to get sequence number from %v: %v", seg, err))
	}
}

func (t *segmentTree) checkProtocolType(seg *segment) {
	protocol := seg.metadata.Protocol()
	switch protocol {
	case openSessionRequest:
	case openSessionResponse:
	case closeSessionRequest:
	case closeSessionResponse:
	case dataClientToServer:
	case dataServerToClient:
	default:
		panic(fmt.Sprintf("Inserting segment with unsupported type %v to the segment tree", protocol))
	}
}
