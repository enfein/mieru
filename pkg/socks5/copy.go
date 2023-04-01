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
	"net"
	"sync/atomic"

	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/stderror"
)

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
				errCh <- fmt.Errorf("ReadFrom UDP connection failed: %w", err)
				break
			}
			UDPAssociateInPkts.Add(1)
			UDPAssociateInBytes.Add(int64(n))
			udpAddr := addr.Load()
			if udpAddr == nil {
				addr.Store(a)
			} else if udpAddr.(net.Addr).String() != a.String() {
				errCh <- fmt.Errorf("ReadFrom new UDP endpoint %s, first use is %s", a.String(), udpAddr.(net.Addr).String())
				UDPAssociateErrors.Add(1)
				break
			}
			if _, err = tunnelConn.Write(buf[:n]); err != nil {
				errCh <- fmt.Errorf("Write tunnel failed: %w", err)
				break
			}
		}
	}()
	go func() {
		buf := make([]byte, 1<<16)
		for {
			n, err := tunnelConn.Read(buf)
			if err != nil {
				errCh <- fmt.Errorf("Read tunnel failed: %w", err)
				break
			}
			udpAddr := addr.Load()
			if udpAddr == nil {
				errCh <- fmt.Errorf("UDP address is not ready")
				break
			}
			ws, err := udpConn.WriteTo(buf[:n], udpAddr.(net.Addr))
			if err != nil {
				errCh <- fmt.Errorf("WriteTo UDP connetion failed: %w", err)
				break
			}
			UDPAssociateOutPkts.Add(1)
			UDPAssociateOutBytes.Add(int64(ws))
		}
	}()

	var err error
	err = <-errCh
	if !stderror.IsEOF(err) && !stderror.IsClosed(err) {
		log.Debugf("BidiCopyUDP() with endpoint %v failed 1: %v", udpConn.LocalAddr(), err)
	}

	tunnelConn.Close()
	udpConn.Close()

	err = <-errCh
	if !stderror.IsEOF(err) && !stderror.IsClosed(err) {
		log.Debugf("BidiCopyUDP() with endpoint %v failed 2: %v", udpConn.LocalAddr(), err)
	}
	return nil
}
