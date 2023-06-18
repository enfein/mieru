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
	"context"
	"fmt"
	"io"
	mrand "math/rand"
	"net"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/stderror"
)

// Mux manages the sessions and underlays.
type Mux struct {
	isClient        bool
	password        []byte // to construct cipher block
	underlays       []Underlay
	listenEndpoints []UnderlayProperties
	multiplexFactor int // to decide if reusing a underlay

	chAccept    chan net.Conn
	chAcceptErr chan error
	done        chan struct{}
}

var _ net.Listener = &Mux{}

// NewMux creates a new mieru v2 multiplex controller.
func NewMux(isClinet bool, password []byte, listenEndpoints []UnderlayProperties, multiplexFactor int) *Mux {
	return &Mux{
		isClient:        isClinet,
		password:        password,
		underlays:       make([]Underlay, 0),
		listenEndpoints: listenEndpoints,
		multiplexFactor: multiplexFactor,
		chAccept:        make(chan net.Conn, 256),
		chAcceptErr:     make(chan error, 1), // non-blocking
		done:            make(chan struct{}),
	}
}

func (m *Mux) Accept() (net.Conn, error) {
	select {
	case err := <-m.chAcceptErr:
		return nil, err
	case conn := <-m.chAccept:
		return conn, nil
	case <-m.done:
		return nil, io.ErrClosedPipe
	}
}

func (m *Mux) Close() error {
	for _, underlay := range m.underlays {
		underlay.Close()
	}
	return nil
}

// Addr is not supported by Mux.
func (m *Mux) Addr() net.Addr {
	return netutil.NilNetAddr
}

// ListenAndServeAll listens on all the server addresses and serves
// incoming requests. Call this method in client results in an error.
func (m *Mux) ListenAndServeAll() error {
	if m.isClient {
		return stderror.ErrInvalidOperation
	}
	if len(m.listenEndpoints) == 0 {
		return fmt.Errorf("no server listening endpoint found")
	}
	return nil
}

// DialContext returns a network connection for the client to consume.
// The connection may be a session established from an existing underlay.
func (m *Mux) DialContext(ctx context.Context) (net.Conn, error) {
	if !m.isClient {
		return nil, stderror.ErrInvalidOperation
	}

	createNewUnderlay := true
	var underlay Underlay
	if m.multiplexFactor > 0 {
		reuseUnderlayFactor := len(m.underlays) * m.multiplexFactor
		n := mrand.Intn(reuseUnderlayFactor + 1)
		if n < reuseUnderlayFactor {
			createNewUnderlay = false
			underlay = m.underlays[n/m.multiplexFactor]
		}
	}
	if createNewUnderlay {
		i := mrand.Intn(len(m.listenEndpoints))
		p := m.listenEndpoints[i]
		switch p.TransportProtocol() {
		case netutil.TCPTransport:
			block, err := cipher.BlockCipherFromPassword(m.password, false)
			if err != nil {
				return nil, fmt.Errorf("cipher.BlockCipherFromPassword() failed: %v", err)
			}
			underlay, err = NewTCPUnderlay(ctx, p.RemoteAddr().Network(), "", p.RemoteAddr().String(), p.MTU(), block)
			if err != nil {
				return nil, fmt.Errorf("NewTCPUnderlay() failed: %v", err)
			}
			log.Debugf("Create new underlay %v", underlay)
		default:
			return nil, fmt.Errorf("unsupport transport protocol %v", p.TransportProtocol())
		}
	} else {
		log.Debugf("Reuse existing underlay %v", underlay)
	}

	session := NewSession(mrand.Uint32(), true, underlay.MTU())
	if err := underlay.AddSession(session); err != nil {
		return nil, fmt.Errorf("AddSession() failed: %v", err)
	}
	return session, nil
}
