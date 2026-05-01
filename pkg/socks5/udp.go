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
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/common"
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

// runUDPAssociateDatagramLoop exchanges RFC 1928 SOCKS5 UDP datagrams between
// a SOCKS5 proxy client and UDP destinations until the TCP control connection
// is closed.
func runUDPAssociateDatagramLoop(udpConn *net.UDPConn, ctrlConn net.Conn, resolver apicommon.DNSResolver) error {
	if resolver == nil {
		resolver = &net.Resolver{}
	}

	var monitorWG sync.WaitGroup
	monitorWG.Add(1)
	go func() {
		defer monitorWG.Done()
		common.ReadAllAndDiscard(ctrlConn)
		udpConn.Close()
	}()
	defer func() {
		ctrlConn.Close()
		udpConn.Close()
		monitorWG.Wait()
	}()

	var clientAddr *net.UDPAddr
	targetAddrs := make(map[string]struct{})
	buf := make([]byte, 1<<16)
	for {
		n, addr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			if !stderror.IsEOF(err) && !stderror.IsClosed(err) {
				return fmt.Errorf("UDP datagram relay %v ReadFromUDP() failed: %w", udpConn.LocalAddr(), err)
			}
			return nil
		}

		if clientAddr == nil || sameUDPAddr(addr, clientAddr) {
			dstAddr, payload, err := parseUDPAssociateDatagram(buf[:n], resolver)
			if err != nil {
				log.Debugf("UDP datagram relay %v dropped invalid packet from %v: %v", udpConn.LocalAddr(), addr, err)
				UDPAssociateErrors.Add(1)
				continue
			}
			if clientAddr == nil {
				clientAddr = cloneUDPAddr(addr)
			}

			ws, err := udpConn.WriteToUDP(payload, dstAddr)
			if err != nil {
				log.Debugf("UDP datagram relay [%v - %v] WriteToUDP() failed: %v", udpConn.LocalAddr(), dstAddr, err)
				UDPAssociateErrors.Add(1)
				continue
			}
			targetAddrs[dstAddr.String()] = struct{}{}
			UDPAssociateUploadPackets.Add(1)
			UDPAssociateUploadBytes.Add(int64(ws))
			continue
		}

		if _, ok := targetAddrs[addr.String()]; !ok {
			log.Debugf("UDP datagram relay %v dropped packet from unexpected endpoint %v", udpConn.LocalAddr(), addr)
			continue
		}

		header := udpAddrToHeader(addr)
		ws, err := udpConn.WriteToUDP(append(header, buf[:n]...), clientAddr)
		if err != nil {
			log.Debugf("UDP datagram relay [%v - %v] WriteToUDP() to client failed: %v", udpConn.LocalAddr(), clientAddr, err)
			UDPAssociateErrors.Add(1)
			continue
		}
		UDPAssociateDownloadPackets.Add(1)
		UDPAssociateDownloadBytes.Add(int64(ws - len(header)))
	}
}

func parseUDPAssociateDatagram(pkt []byte, resolver apicommon.DNSResolver) (*net.UDPAddr, []byte, error) {
	if len(pkt) <= 6 {
		return nil, nil, stderror.ErrNoEnoughData
	}
	if pkt[0] != 0x00 || pkt[1] != 0x00 {
		return nil, nil, stderror.ErrInvalidArgument
	}
	if pkt[2] != 0x00 {
		return nil, nil, stderror.ErrUnsupported
	}

	r := bytes.NewReader(pkt[3:])
	dst := &model.AddrSpec{}
	if err := dst.ReadFromSocks5(r); err != nil {
		return nil, nil, err
	}
	headerLen := len(pkt) - r.Len()
	dstAddr, err := resolveUDPAssociateDatagramAddr(context.Background(), resolver, dst)
	if err != nil {
		return nil, nil, err
	}
	return dstAddr, pkt[headerLen:], nil
}

func resolveUDPAssociateDatagramAddr(ctx context.Context, resolver apicommon.DNSResolver, addr *model.AddrSpec) (*net.UDPAddr, error) {
	if addr.IP.To4() != nil || addr.IP.To16() != nil {
		return &net.UDPAddr{IP: addr.IP, Port: addr.Port}, nil
	}
	if addr.FQDN != "" {
		return apicommon.ResolveUDPAddr(ctx, resolver, "udp", addr.String())
	}
	return nil, model.ErrUnrecognizedAddrType
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	return &net.UDPAddr{
		IP:   append(net.IP(nil), addr.IP...),
		Port: addr.Port,
		Zone: addr.Zone,
	}
}

func sameUDPAddr(a, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Port == b.Port && a.Zone == b.Zone && a.IP.Equal(b.IP)
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
