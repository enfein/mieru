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
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"

	apicommon "github.com/enfein/mieru/v3/apis/common"
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

			datagram, err := parseSocks5UDPDatagram(buf[:n])
			if err != nil {
				udpErr.Store(err)
				UDPAssociateErrors.Add(1)
				return
			}
			dstAddr, err := resolveSocks5UDPAddr(context.Background(), resolver, datagram.Addr)
			if err != nil {
				log.Debugf("UDP associate %v ResolveUDPAddr() failed: %v", udpConn.LocalAddr(), err)
				UDPAssociateErrors.Add(1)
				continue
			}
			addrMap.Store(dstAddr.String(), datagram.Header)
			ws, err := udpConn.WriteToUDP(datagram.Payload, dstAddr)
			if err != nil {
				log.Debugf("UDP associate [%v - %v] WriteToUDP() failed: %v", udpConn.LocalAddr(), dstAddr, err)
				UDPAssociateErrors.Add(1)
			} else {
				UDPAssociateUploadPackets.Add(1)
				UDPAssociateUploadBytes.Add(int64(ws))
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
			_, err = conn.Write(append(append([]byte(nil), header...), buf[:n]...))
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
				clientAddr = &net.UDPAddr{
					IP:   append(net.IP(nil), addr.IP...),
					Port: addr.Port,
					Zone: addr.Zone,
				}
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

func rewriteSocks5ResponseBindPort(resp *model.Response, port int) error {
	if resp == nil {
		return fmt.Errorf("socks5 response is nil")
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("port %d is out of range", port)
	}
	resp.BindAddr.Port = port
	if len(resp.Raw) >= 2 {
		resp.Raw[len(resp.Raw)-2] = byte(port >> 8)
		resp.Raw[len(resp.Raw)-1] = byte(port)
		return nil
	}

	var buf bytes.Buffer
	if err := resp.WriteToSocks5(&buf); err != nil {
		return err
	}
	resp.Raw = buf.Bytes()
	return nil
}

func socks5UDPAddrFromResponse(conn net.Conn, resp *model.Response) (*net.UDPAddr, error) {
	tcpRemote, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("socks5 proxy remote address has unexpected type %T", conn.RemoteAddr())
	}
	port := resp.BindAddr.Port
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("socks5 UDP bind port %d is invalid", port)
	}
	ip := resp.BindAddr.IP
	if ip == nil || ip.IsUnspecified() {
		ip = tcpRemote.IP
	}
	if ip == nil || ip.IsUnspecified() {
		return nil, fmt.Errorf("socks5 UDP bind address is unspecified")
	}
	return &net.UDPAddr{IP: ip, Port: port}, nil
}

type socks5UDPDatagram struct {
	Addr    model.AddrSpec
	Header  []byte
	Payload []byte
}

func parseSocks5UDPDatagram(pkt []byte) (*socks5UDPDatagram, error) {
	if len(pkt) <= 6 {
		return nil, stderror.ErrNoEnoughData
	}
	if pkt[0] != 0x00 || pkt[1] != 0x00 {
		return nil, stderror.ErrInvalidArgument
	}
	if pkt[2] != 0x00 {
		return nil, stderror.ErrUnsupported
	}

	r := bytes.NewReader(pkt[3:])
	dst := model.AddrSpec{}
	if err := dst.ReadFromSocks5(r); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, stderror.ErrNoEnoughData
		}
		return nil, err
	}
	headerLen := len(pkt) - r.Len()
	if len(pkt) < headerLen {
		return nil, stderror.ErrNoEnoughData
	}

	return &socks5UDPDatagram{
		Addr:    dst,
		Header:  append([]byte(nil), pkt[:headerLen]...),
		Payload: pkt[headerLen:],
	}, nil
}

func newSocks5UDPDatagram(addr model.AddrSpec, payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write([]byte{0, 0, 0})
	if err := addr.WriteToSocks5(&buf); err != nil {
		return nil, err
	}
	buf.Write(payload)
	return buf.Bytes(), nil
}

func resolveSocks5UDPAddr(ctx context.Context, resolver apicommon.DNSResolver, addr model.AddrSpec) (*net.UDPAddr, error) {
	if addr.IP.To4() != nil || addr.IP.To16() != nil {
		return &net.UDPAddr{IP: addr.IP, Port: addr.Port}, nil
	}
	if addr.FQDN != "" {
		return apicommon.ResolveUDPAddr(ctx, resolver, "udp", addr.String())
	}
	return nil, model.ErrUnrecognizedAddrType
}

func parseUDPAssociateDatagram(pkt []byte, resolver apicommon.DNSResolver) (*net.UDPAddr, []byte, error) {
	datagram, err := parseSocks5UDPDatagram(pkt)
	if err != nil {
		return nil, nil, err
	}
	dstAddr, err := resolveSocks5UDPAddr(context.Background(), resolver, datagram.Addr)
	if err != nil {
		return nil, nil, err
	}
	return dstAddr, datagram.Payload, nil
}

func udpAddrSpec(addr *net.UDPAddr) model.AddrSpec {
	return model.AddrSpec{
		IP:   addr.IP,
		Port: addr.Port,
	}
}

// udpAddrToHeader returns a UDP associate header with the given
// destination address.
func udpAddrToHeader(addr *net.UDPAddr) []byte {
	if addr == nil {
		panic("When translating UDP address to UDP associate header, the UDP address is nil")
	}
	header, err := newSocks5UDPDatagram(udpAddrSpec(addr), nil)
	if err != nil {
		panic(err)
	}
	return header
}

func wrapSocks5UDPPacket(addr net.Addr, payload []byte) ([]byte, error) {
	var netAddr model.NetAddrSpec
	if err := netAddr.From(addr); err != nil {
		return nil, err
	}
	return newSocks5UDPDatagram(netAddr.AddrSpec, payload)
}

func unwrapSocks5UDPPacket(pkt []byte) ([]byte, error) {
	datagram, err := parseSocks5UDPDatagram(pkt)
	if err != nil {
		return nil, err
	}
	return datagram.Payload, nil
}

func localUDPPort(conn *net.UDPConn) (int, error) {
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return 0, fmt.Errorf("UDP listener local address has unexpected type %T", conn.LocalAddr())
	}
	return local.Port, nil
}

func sameUDPAddr(a, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Port == b.Port && a.IP.Equal(b.IP)
}
