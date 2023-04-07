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
// along with this program.  If not, see <https://www.gnu.org/licenses/>

package protocolv2

import (
	"sync"

	"github.com/enfein/mieru/pkg/bimap"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/stderror"
)

// baseUnderlay contains a base implementation of underlay.
type baseUnderlay struct {
	isClient bool
	mtu      int
	die      chan struct{} // if the underlay is closed

	// Map<sessionID, *Session>.
	sessionMap map[uint32]*Session

	// Map<requestID, *Session>.
	pendingSessionMap map[uint32]*Session

	// BiMap<requestID, sessionID>.
	sessionIDMap *bimap.BiMap[uint32, uint32]

	sessionLock sync.Mutex
}

var (
	_ Underlay = &baseUnderlay{}
)

func newBaseUnderlay(isClient bool, mtu int) *baseUnderlay {
	return &baseUnderlay{
		isClient:          isClient,
		mtu:               mtu,
		die:               make(chan struct{}),
		sessionMap:        make(map[uint32]*Session),
		pendingSessionMap: make(map[uint32]*Session),
		sessionIDMap:      bimap.NewBiMap[uint32, uint32](),
	}
}

func (b *baseUnderlay) MTU() int {
	return b.mtu
}

func (b *baseUnderlay) IPVersion() netutil.IPVersion {
	return netutil.IPVersionUnknown
}

func (b *baseUnderlay) TransportProtocol() netutil.TransportProtocol {
	return netutil.UnknownTransport
}

func (b *baseUnderlay) AddSession(s *Session) error {
	if s == nil {
		return stderror.ErrNullPointer
	}
	b.sessionLock.Lock()
	defer b.sessionLock.Unlock()
	_, found := b.sessionMap[s.id]
	if found {
		return stderror.ErrAlreadyExist
	}
	b.sessionMap[s.id] = s
	s.conn = b
	return nil
}

func (b *baseUnderlay) RemoveSession(s *Session) error {
	if s == nil {
		return stderror.ErrNullPointer
	}
	b.sessionLock.Lock()
	defer b.sessionLock.Unlock()
	delete(b.sessionMap, s.id)
	s.conn = nil
	return nil
}

func (b *baseUnderlay) RunEventLoop() error {
	return stderror.ErrUnsupported
}

func (b *baseUnderlay) Close() error {
	b.sessionLock.Lock()
	defer b.sessionLock.Unlock()
	for _, p := range b.pendingSessionMap {
		p.Close()
	}
	for _, s := range b.sessionMap {
		s.Close()
	}
	return nil
}
