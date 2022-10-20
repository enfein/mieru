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

package socks5

import (
	"fmt"
	"io"
	"net"
	"sync/atomic"

	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
)

type closeWriter interface {
	CloseWrite() error
}

// BidiCopy does bi-directional data copy.
func BidiCopy(conn1, conn2 io.ReadWriteCloser, isClient bool) error {
	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn1, conn2)
		if isClient {
			// Must call Close() to make sure counter is updated.
			conn1.Close()
		} else {
			// Avoid counter updated twice due to twice Close(), use CloseWrite().
			// The connection still needs to close here to unblock the other side.
			if tcpConn1, ok := conn1.(closeWriter); ok {
				tcpConn1.CloseWrite()
			}
		}
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(conn2, conn1)
		if isClient {
			conn2.Close()
		} else {
			if tcpConn2, ok := conn2.(closeWriter); ok {
				tcpConn2.CloseWrite()
			}
		}
		errCh <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}
	return nil
}

// BidiCopyUDP does bi-directional data copy between a proxy client UDP endpoint
// and the proxy tunnel.
func BidiCopyUDP(udpConn *net.UDPConn, tunnelConn *UDPAssociateTunnelConn) error {
	var addr atomic.Value
	errCh := make(chan error, 2)

	go func() {
		buf := make([]byte, 1<<16)
		for {
			n, a, err := udpConn.ReadFrom(buf)
			if err != nil {
				// This is typically due to close of UDP listener.
				// Don't contribute to metrics.Socks5UDPAssociateErrors.
				errCh <- fmt.Errorf("ReadFrom UDP connection failed: %v", err)
				break
			}
			atomic.AddUint64(&metrics.UDPAssociateInPkts, 1)
			atomic.AddUint64(&metrics.UDPAssociateInBytes, uint64(n))
			udpAddr := addr.Load()
			if udpAddr == nil {
				addr.Store(a)
			} else if udpAddr.(net.Addr).String() != a.String() {
				errCh <- fmt.Errorf("ReadFrom new UDP endpoint %s, first use is %s", a.String(), udpAddr.(net.Addr).String())
				atomic.AddUint64(&metrics.Socks5UDPAssociateErrors, 1)
				break
			}
			if _, err = tunnelConn.Write(buf[:n]); err != nil {
				errCh <- fmt.Errorf("Write tunnel failed: %v", err)
				break
			}
		}
	}()
	go func() {
		buf := make([]byte, 1<<16)
		for {
			n, err := tunnelConn.Read(buf)
			if err != nil {
				errCh <- fmt.Errorf("Read tunnel failed: %v", err)
				break
			}
			udpAddr := addr.Load()
			if udpAddr == nil {
				errCh <- fmt.Errorf("UDP address is not ready")
				break
			}
			ws, err := udpConn.WriteTo(buf[:n], udpAddr.(net.Addr))
			if err != nil {
				errCh <- fmt.Errorf("WriteTo UDP connetion failed: %v", err)
				break
			}
			atomic.AddUint64(&metrics.UDPAssociateOutPkts, 1)
			atomic.AddUint64(&metrics.UDPAssociateOutBytes, uint64(ws))
		}
	}()

	var err error
	err = <-errCh
	log.Debugf("BidiCopyUDP() with endpoint %v failed 1: %v", udpConn.LocalAddr(), err)

	tunnelConn.Close()
	udpConn.Close()

	err = <-errCh
	log.Debugf("BidiCopyUDP() with endpoint %v failed 2: %v", udpConn.LocalAddr(), err)
	return nil
}
