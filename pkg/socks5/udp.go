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
	"context"
	"encoding/binary"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

// RunUDPAssociateLoop exchanges socks5 UDP packets between a socks5 proxy client and a mieru proxy server,
// the proxy server is connected via the PacketOverStreamTunnel.
func RunUDPAssociateLoop(udpConn *net.UDPConn, conn *apicommon.PacketOverStreamTunnel, resolver apicommon.DNSResolver) error {
	var udpErr atomic.Value

	// addrMap maps the UDPAddr in string to the bytes in UDP associate header.
	var addrMap sync.Map

	var wg sync.WaitGroup
	wg.Add(2)

	// Packets from socks5 proxy client -> mieru proxy server.
	go func() {
		defer wg.Done()
		defer udpConn.Close()
		buf := make([]byte, 1<<16)
		var n int
		var err error
		for {
			n, err = conn.Read(buf)
			if err != nil {
				udpErr.Store(err)
				return
			}

			// Validate received UDP request.
			if n <= 6 {
				udpErr.Store(stderror.ErrNoEnoughData)
				UDPAssociateErrors.Add(1)
				return
			}
			if buf[0] != 0x00 || buf[1] != 0x00 {
				udpErr.Store(stderror.ErrInvalidArgument)
				UDPAssociateErrors.Add(1)
				return
			}
			if buf[2] != 0x00 {
				// UDP fragment is not supported.
				udpErr.Store(stderror.ErrUnsupported)
				UDPAssociateErrors.Add(1)
				return
			}
			addrType := buf[3]
			if addrType != constant.Socks5IPv4Address && addrType != constant.Socks5FQDNAddress && addrType != constant.Socks5IPv6Address {
				udpErr.Store(stderror.ErrInvalidArgument)
				UDPAssociateErrors.Add(1)
				return
			}
			if (addrType == constant.Socks5IPv4Address && n <= 10) || (addrType == constant.Socks5FQDNAddress && n <= int(buf[4])+6) || (addrType == constant.Socks5IPv6Address && n <= 22) {
				udpErr.Store(stderror.ErrNoEnoughData)
				UDPAssociateErrors.Add(1)
				return
			}

			// Get target address and send data.
			switch addrType {
			case constant.Socks5IPv4Address:
				dstAddr := &net.UDPAddr{
					IP:   net.IP(buf[4:8]),
					Port: int(buf[8])<<8 + int(buf[9]),
				}
				addrMap.Store(dstAddr.String(), buf[:10])
				ws, err := udpConn.WriteToUDP(buf[10:n], dstAddr)
				if err != nil {
					log.Debugf("UDP associate [%v - %v] WriteToUDP() failed: %v", udpConn.LocalAddr(), dstAddr, err)
					UDPAssociateErrors.Add(1)
				} else {
					UDPAssociateUploadPackets.Add(1)
					UDPAssociateUploadBytes.Add(int64(ws))
				}
			case constant.Socks5FQDNAddress:
				fqdnLen := buf[4]
				fqdn := string(buf[5 : 5+fqdnLen])
				dstAddr, err := apicommon.ResolveUDPAddr(context.Background(), resolver, "udp", fqdn+":"+strconv.Itoa(int(buf[5+fqdnLen])<<8+int(buf[6+fqdnLen])))
				if err != nil {
					log.Debugf("UDP associate %v ResolveUDPAddr() failed: %v", udpConn.LocalAddr(), err)
					UDPAssociateErrors.Add(1)
					break
				}
				addrMap.Store(dstAddr.String(), buf[:7+fqdnLen])
				ws, err := udpConn.WriteToUDP(buf[7+fqdnLen:n], dstAddr)
				if err != nil {
					log.Debugf("UDP associate [%v - %v] WriteToUDP() failed: %v", udpConn.LocalAddr(), dstAddr, err)
					UDPAssociateErrors.Add(1)
				} else {
					UDPAssociateUploadPackets.Add(1)
					UDPAssociateUploadBytes.Add(int64(ws))
				}
			case constant.Socks5IPv6Address:
				dstAddr := &net.UDPAddr{
					IP:   net.IP(buf[4:20]),
					Port: int(buf[20])<<8 + int(buf[21]),
				}
				addrMap.Store(dstAddr.String(), buf[:22])
				ws, err := udpConn.WriteToUDP(buf[22:n], dstAddr)
				if err != nil {
					log.Debugf("UDP associate [%v - %v] WriteToUDP() failed: %v", udpConn.LocalAddr(), dstAddr, err)
					UDPAssociateErrors.Add(1)
				} else {
					UDPAssociateUploadPackets.Add(1)
					UDPAssociateUploadBytes.Add(int64(ws))
				}
			}
		}
	}()

	// Packets from mieru proxy server -> socks5 proxy client.
	go func() {
		defer wg.Done()
		buf := make([]byte, 1<<16)
		var n int
		var addr *net.UDPAddr
		var err error
		for {
			n, addr, err = udpConn.ReadFromUDP(buf)
			if err != nil {
				// This is typically due to close of UDP listener.
				// Don't contribute to UDPAssociateErrors.
				if !stderror.IsEOF(err) && !stderror.IsClosed(err) {
					log.Debugf("UDP associate %v Read() failed: %v", udpConn.LocalAddr(), err)
				}
				if udpErr.Load() == nil {
					udpErr.Store(err)
				}
				return
			}
			var header []byte
			v, ok := addrMap.Load(addr.String())
			if ok {
				header = v.([]byte)
			} else {
				header = udpAddrToHeader(addr)
				addrMap.Store(addr.String(), header)
			}
			_, err = conn.Write(append(header, buf[:n]...))
			if err != nil {
				log.Debugf("UDP associate %v Write() to proxy client failed: %v", udpConn.LocalAddr(), err)
				if udpErr.Load() == nil {
					udpErr.Store(err)
				}
				return
			}
			UDPAssociateDownloadPackets.Add(1)
			UDPAssociateDownloadBytes.Add(int64(n))
		}
	}()

	wg.Wait()
	return udpErr.Load().(error)
}

// RunUDPForwardingLoop exchanges socks5 UDP packets between a mieru proxy client and a socks5 proxy server,
// the proxy client is connected via the PacketOverStreamTunnel.
func RunUDPForwardingLoop(udpConn *net.UDPConn, conn *apicommon.PacketOverStreamTunnel, downstreamAddr *net.UDPAddr, ctrlConn net.Conn) error {
	var udpErr atomic.Value

	var wg sync.WaitGroup
	wg.Add(3)

	// Monitor TCP connection for closure (signals end of socks5 UDP association).
	go func() {
		defer wg.Done()
		buf := make([]byte, 1)
		_, err := ctrlConn.Read(buf)
		if err != nil {
			if udpErr.Load() == nil {
				udpErr.Store(err)
			}
		}
		udpConn.Close()
		conn.Close()
	}()

	// Packets from mieru proxy client -> socks5 proxy server.
	go func() {
		defer wg.Done()
		defer udpConn.Close()
		buf := make([]byte, 1<<16)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				if udpErr.Load() == nil {
					udpErr.Store(err)
				}
				return
			}
			ws, err := udpConn.WriteToUDP(buf[:n], downstreamAddr)
			if err != nil {
				log.Debugf("UDP forwarding [%v - %v] WriteToUDP() failed: %v", udpConn.LocalAddr(), downstreamAddr, err)
				UDPAssociateErrors.Add(1)
			} else {
				UDPAssociateUploadPackets.Add(1)
				UDPAssociateUploadBytes.Add(int64(ws))
			}
		}
	}()

	// Packets from socks5 proxy server -> mieru proxy client.
	go func() {
		defer wg.Done()
		buf := make([]byte, 1<<16)
		for {
			n, _, err := udpConn.ReadFromUDP(buf)
			if err != nil {
				if !stderror.IsEOF(err) && !stderror.IsClosed(err) {
					log.Debugf("UDP forwarding %v ReadFromUDP() failed: %v", udpConn.LocalAddr(), err)
				}
				if udpErr.Load() == nil {
					udpErr.Store(err)
				}
				return
			}
			_, err = conn.Write(buf[:n])
			if err != nil {
				log.Debugf("UDP forwarding %v Write() to client failed: %v", udpConn.LocalAddr(), err)
				if udpErr.Load() == nil {
					udpErr.Store(err)
				}
				return
			}
			UDPAssociateDownloadPackets.Add(1)
			UDPAssociateDownloadBytes.Add(int64(n))
		}
	}()

	wg.Wait()
	ctrlConn.Close()
	if err := udpErr.Load(); err != nil {
		return err.(error)
	}
	return nil
}

// udpAddrToHeader returns a UDP associate header with the given
// destination address.
func udpAddrToHeader(addr *net.UDPAddr) []byte {
	if addr == nil {
		panic("When translating UDP address to UDP associate header, the UDP address is nil")
	}
	res := []byte{0, 0, 0}
	ip := addr.IP
	if ip.To4() != nil {
		res = append(res, 1)
		res = append(res, ip.To4()...)
	} else {
		res = append(res, 4)
		res = append(res, ip.To16()...)
	}
	return binary.BigEndian.AppendUint16(res, uint16(addr.Port))
}
