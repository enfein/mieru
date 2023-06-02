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
	"context"
	"fmt"
	"sync"

	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/stderror"
)

// baseUnderlay contains a base implementation of underlay.
type baseUnderlay struct {
	isClient bool
	mtu      int
	done     chan struct{} // if the underlay is closed

	// Map<sessionID, *Session>.
	sessionMap  map[uint32]*Session
	sessionLock sync.Mutex
}

var (
	_ Underlay = &baseUnderlay{}
)

func newBaseUnderlay(isClient bool, mtu int) *baseUnderlay {
	return &baseUnderlay{
		isClient:   isClient,
		mtu:        mtu,
		done:       make(chan struct{}),
		sessionMap: make(map[uint32]*Session),
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
	if s.id == 0 {
		return fmt.Errorf("session ID can't be 0")
	}
	if s.state >= sessionAttached {
		return fmt.Errorf("session %d is already attached to a underlay", s.id)
	}
	b.sessionLock.Lock()
	defer b.sessionLock.Unlock()
	if s.id != 0 {
		_, found := b.sessionMap[s.id]
		if found {
			return stderror.ErrAlreadyExist
		}
		b.sessionMap[s.id] = s
	}
	s.conn = b
	s.state = sessionAttached
	return nil
}

func (b *baseUnderlay) RemoveSession(s *Session) error {
	if s == nil {
		return stderror.ErrNullPointer
	}
	if s.state < sessionAttached {
		return fmt.Errorf("session %d is not attached to this underlay", s.id)
	}
	b.sessionLock.Lock()
	defer b.sessionLock.Unlock()
	delete(b.sessionMap, s.id)
	s.conn = nil
	return nil
}

func (b *baseUnderlay) RunEventLoop(ctx context.Context) error {
	return stderror.ErrUnsupported
}

func (b *baseUnderlay) Close() error {
	b.sessionLock.Lock()
	defer b.sessionLock.Unlock()
	for _, s := range b.sessionMap {
		s.Close()
	}
	close(b.done)
	return nil
}
