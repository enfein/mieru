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

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

// BidiCopyUDP does bi-directional data copy between a proxy client UDP endpoint
// and the proxy tunnel.
func BidiCopyUDP(udpConn *net.UDPConn, tunnelConn *apicommon.PacketOverStreamTunnel) error {
	var addr atomic.Value
	errCh := make(chan error, 2)

	go func() {
		// udpConn -> tunnelConn
		buf := make([]byte, 1<<16)
		for {
			n, a, err := udpConn.ReadFrom(buf)
			if err != nil {
				// This is typically due to close of UDP listener.
				// Don't contribute to metrics.Socks5UDPAssociateErrors.
				errCh <- fmt.Errorf("ReadFrom UDP connection failed: %w", err)
				break
			}
			UDPAssociateUploadPackets.Add(1)
			UDPAssociateUploadBytes.Add(int64(n))
			udpAddr := addr.Load()
			if udpAddr == nil {
				addr.Store(a)
			} else if udpAddr.(net.Addr).String() != a.String() {
				errCh <- fmt.Errorf("ReadFrom new UDP endpoint %s, first use is %s", a.String(), udpAddr.(net.Addr).String())
				UDPAssociateErrors.Add(1)
				break
			}
			if _, err = tunnelConn.Write(buf[:n]); err != nil {
				errCh <- fmt.Errorf("write tunnel failed: %w", err)
				break
			}
		}
	}()
	go func() {
		// tunnelConn -> udpConn
		buf := make([]byte, 1<<16)
		for {
			n, err := tunnelConn.Read(buf)
			if err != nil {
				errCh <- fmt.Errorf("read tunnel failed: %w", err)
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
			UDPAssociateDownloadPackets.Add(1)
			UDPAssociateDownloadBytes.Add(int64(ws))
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

// BidiCopySocks5 does bi-directional data copy.
// This function is aware of socks5 protocol and doesn't copy the socks5 reply header
// sent by the server.
// If pendingReq is not empty, it is prepended to proxyConn.
func BidiCopySocks5(conn, proxyConn io.ReadWriteCloser, pendingReq []byte) error {
	errCh := make(chan error, 2)

	go func() {
		// conn -> proxyConn
		buf := make([]byte, 1<<16)
		reqLen := len(pendingReq)
		if reqLen > 0 {
			log.Debugf("HANDSHAKE_NO_WAIT mode socks5 client request: %v", pendingReq)
			reqLen = copy(buf, pendingReq)
		}
		n, err := conn.Read(buf[reqLen:])
		if err != nil {
			proxyConn.Close()
			errCh <- fmt.Errorf("failed to read connection request from the client: %w", err)
			return
		}
		n += reqLen
		_, err = proxyConn.Write(buf[:n])
		if err != nil {
			proxyConn.Close()
			errCh <- fmt.Errorf("failed to write connection request to the server: %w", err)
			return
		}
		_, err = io.Copy(proxyConn, conn)
		proxyConn.Close()
		errCh <- err
	}()

	go func() {
		// proxyConn -> conn
		connResp := make([]byte, 4)
		if _, err := io.ReadFull(proxyConn, connResp); err != nil {
			conn.Close()
			errCh <- fmt.Errorf("failed to read connection response from the server: %w", err)
			return
		}
		respAddrType := connResp[3]
		var respFQDNLen []byte
		var bindAddr []byte
		switch respAddrType {
		case constant.Socks5IPv4Address:
			bindAddr = make([]byte, 6)
		case constant.Socks5FQDNAddress:
			respFQDNLen = []byte{0}
			if _, err := io.ReadFull(proxyConn, respFQDNLen); err != nil {
				conn.Close()
				errCh <- fmt.Errorf("failed to get FQDN length: %w", err)
				return
			}
			bindAddr = make([]byte, respFQDNLen[0]+2)
		case constant.Socks5IPv6Address:
			bindAddr = make([]byte, 18)
		default:
			conn.Close()
			errCh <- fmt.Errorf("unsupported address type: %d", respAddrType)
			return
		}
		if _, err := io.ReadFull(proxyConn, bindAddr); err != nil {
			conn.Close()
			errCh <- fmt.Errorf("failed to get bind address: %w", err)
			return
		}
		if len(respFQDNLen) != 0 {
			connResp = append(connResp, respFQDNLen...)
		}
		connResp = append(connResp, bindAddr...)
		log.Debugf("HANDSHAKE_NO_WAIT mode socks5 server reply: %v", connResp)
		_, err := io.Copy(conn, proxyConn)
		conn.Close()
		errCh <- err
	}()

	err := <-errCh
	<-errCh
	return err
}
