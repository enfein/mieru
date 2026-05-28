// Copyright (C) 2026  mieru authors
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

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

func zeroBindAddr() model.AddrSpec {
	return model.AddrSpec{IP: net.IPv4(0, 0, 0, 0)}
}

func localUDPPort(conn *net.UDPConn) (int, error) {
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return 0, fmt.Errorf("UDP listener local address has unexpected type %T", conn.LocalAddr())
	}
	return local.Port, nil
}

func listenUDP4() (*net.UDPConn, error) {
	return net.ListenUDP("udp4", &net.UDPAddr{IP: net.IP{0, 0, 0, 0}, Port: 0})
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
	if len(netAddr.FQDN) > 255 {
		return nil, fmt.Errorf("FQDN %q exceeds 255 bytes", netAddr.FQDN)
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
