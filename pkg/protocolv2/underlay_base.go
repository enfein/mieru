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
	"io"
	"net"
	"sync"

	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/stderror"
	"github.com/enfein/mieru/pkg/util"
)

// baseUnderlay contains a base implementation of underlay.
type baseUnderlay struct {
	isClient bool
	mtu      int
	done     chan struct{} // if the underlay is closed

	sessionMap    sync.Map      // Map<sessionID, *Session>
	readySessions chan *Session // sessions that completed handshake and ready for consume
}

var (
	_ Underlay = &baseUnderlay{}
)

func newBaseUnderlay(isClient bool, mtu int) *baseUnderlay {
	return &baseUnderlay{
		isClient:      isClient,
		mtu:           mtu,
		done:          make(chan struct{}),
		readySessions: make(chan *Session, 256),
	}
}

func (b *baseUnderlay) Accept() (net.Conn, error) {
	select {
	case session := <-b.readySessions:
		return session, nil
	case <-b.done:
		return nil, io.ErrClosedPipe
	}
}

func (b *baseUnderlay) Close() error {
	select {
	case <-b.done:
		return nil
	default:
	}

	b.sessionMap.Range(func(k, v any) bool {
		s := v.(*Session)
		s.Close()
		return true
	})
	b.sessionMap = sync.Map{}
	close(b.done)
	return nil
}

func (b *baseUnderlay) Addr() net.Addr {
	return util.NilNetAddr()
}

func (b *baseUnderlay) MTU() int {
	return b.mtu
}

func (b *baseUnderlay) IPVersion() util.IPVersion {
	return util.IPVersionUnknown
}

func (b *baseUnderlay) TransportProtocol() util.TransportProtocol {
	return util.UnknownTransport
}

func (b *baseUnderlay) LocalAddr() net.Addr {
	return util.NilNetAddr()
}

func (b *baseUnderlay) RemoteAddr() net.Addr {
	return util.NilNetAddr()
}

func (b *baseUnderlay) AddSession(s *Session, remoteAddr net.Addr) error {
	if s == nil {
		return stderror.ErrNullPointer
	}
	if s.id == 0 {
		return fmt.Errorf("session ID can't be 0")
	}
	if s.state >= sessionAttached {
		return fmt.Errorf("session %d is already attached to a underlay", s.id)
	}
	if b.isClient && !s.isClient {
		return fmt.Errorf("can't add a server session to a client underlay")
	}
	if !b.isClient && s.isClient {
		return fmt.Errorf("can't add a client session to a server underlay")
	}

	if s.id != 0 {
		if _, loaded := b.sessionMap.LoadOrStore(s.id, s); loaded {
			return stderror.ErrAlreadyExist
		}
	}
	s.conn = b
	s.remoteAddr = remoteAddr
	s.forwardStateTo(sessionAttached)

	if s.isClient {
		metrics.ActiveOpens.Add(1)
	} else {
		metrics.PassiveOpens.Add(1)
	}
	currEst := metrics.CurrEstablished.Add(1)
	maxConn := metrics.MaxConn.Load()
	if currEst > maxConn {
		metrics.MaxConn.Store(currEst)
	}
	return nil
}

func (b *baseUnderlay) RemoveSession(s *Session) error {
	if s == nil {
		return stderror.ErrNullPointer
	}
	if s.state < sessionAttached {
		return fmt.Errorf("session %d is not attached to this underlay", s.id)
	}

	b.sessionMap.Delete(s.id)
	s.conn = nil
	s.forwardStateTo(sessionClosed)
	metrics.CurrEstablished.Add(-1)
	return nil
}

func (b *baseUnderlay) RunEventLoop(ctx context.Context) error {
	return stderror.ErrUnsupported
}

func (b *baseUnderlay) Done() chan struct{} {
	return b.done
}
