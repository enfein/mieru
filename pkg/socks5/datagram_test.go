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
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
)

type datagramFailResolver struct{}

func (r datagramFailResolver) LookupIP(_ context.Context, _ string, host string) ([]net.IP, error) {
	return nil, fmt.Errorf("unexpected DNS lookup for %s", host)
}

func TestDatagramModeNoAuthNegotiation(t *testing.T) {
	proxyAddr := startDatagramModeServer(t, nil)
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("net.Dial() failed: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte{constant.Socks5Version, 1, constant.Socks5NoAuth}); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	out := make([]byte, 2)
	if _, err := io.ReadFull(conn, out); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}
	want := []byte{constant.Socks5Version, constant.Socks5NoAuth}
	if !bytes.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}
}

func TestDatagramModeConnect(t *testing.T) {
	target := startTCPEchoServer(t)
	proxyAddr := startDatagramModeServer(t, nil)

	dialer := DialSocks5Proxy(&Client{
		Host:    proxyAddr,
		CmdType: constant.Socks5ConnectCmd,
	})
	conn, _, _, err := dialer("tcp", target.String())
	if err != nil {
		t.Fatalf("dial to socks5 proxy failed: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	out := make([]byte, 4)
	if _, err := io.ReadFull(conn, out); err != nil {
		t.Fatalf("io.ReadFull() failed: %v", err)
	}
	if !bytes.Equal(out, []byte("pong")) {
		t.Fatalf("got %v, want %v", out, []byte("pong"))
	}
}

func TestDatagramModeUDPAssociate(t *testing.T) {
	target := startUDPEchoServer(t)
	proxyAddr := startDatagramModeServer(t, nil)

	ctrlConn, udpConn, proxyUDPAddr := dialUDPAssociate(t, proxyAddr, target.String())
	defer ctrlConn.Close()
	defer udpConn.Close()

	udpConn.SetReadDeadline(time.Now().Add(time.Second))
	defer udpConn.SetReadDeadline(time.Time{})
	out, err := TransceiveUDPPacket(udpConn, proxyUDPAddr, target, []byte("ping"))
	if err != nil {
		t.Fatalf("TransceiveUDPPacket() failed: %v", err)
	}
	if !bytes.Equal(out, []byte("pong")) {
		t.Fatalf("got %v, want %v", out, []byte("pong"))
	}
}

func TestDatagramModeDropsFragmentedUDPPackets(t *testing.T) {
	targetAddr, received := startUDPRecordingServer(t)
	proxyAddr := startDatagramModeServer(t, nil)

	ctrlConn, udpConn, proxyUDPAddr := dialUDPAssociate(t, proxyAddr, targetAddr.String())
	defer ctrlConn.Close()
	defer udpConn.Close()

	fragPacket := []byte{0, 0, 1, constant.Socks5IPv4Address, 127, 0, 0, 1, byte(targetAddr.Port >> 8), byte(targetAddr.Port), 'p', 'i', 'n', 'g'}
	if _, err := udpConn.WriteToUDP(fragPacket, proxyUDPAddr); err != nil {
		t.Fatalf("WriteToUDP() failed: %v", err)
	}

	udpConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	defer udpConn.SetReadDeadline(time.Time{})
	buf := make([]byte, 1024)
	if n, _, err := udpConn.ReadFromUDP(buf); err == nil {
		t.Fatalf("got unexpected UDP response %v", buf[:n])
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("ReadFromUDP() error = %v, want timeout", err)
	}

	select {
	case got := <-received:
		t.Fatalf("fragmented datagram was forwarded to target: %v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDatagramModeUDPAssociateFQDNDestination(t *testing.T) {
	target := startUDPEchoServer(t)
	const host = "udp-associate.example"
	resolver := apicommon.HostMapResolver{
		Resolver: datagramFailResolver{},
		Hosts: map[string]net.IP{
			apicommon.NormalizeDomainName(host): net.IPv4(127, 0, 0, 1),
		},
	}
	proxyAddr := startDatagramModeServer(t, resolver)

	ctrlConn, udpConn, proxyUDPAddr := dialUDPAssociate(t, proxyAddr, target.String())
	defer ctrlConn.Close()
	defer udpConn.Close()

	udpConn.SetReadDeadline(time.Now().Add(time.Second))
	defer udpConn.SetReadDeadline(time.Time{})
	out, err := transceiveUDPFQDNPacket(udpConn, proxyUDPAddr, host, target.Port, []byte("ping"))
	if err != nil {
		t.Fatalf("transceiveUDPFQDNPacket() failed: %v", err)
	}
	if !bytes.Equal(out, []byte("pong")) {
		t.Fatalf("got %v, want %v", out, []byte("pong"))
	}
}

func startDatagramModeServer(t *testing.T, resolver apicommon.DNSResolver) string {
	t.Helper()
	conf := &Config{
		AllowLoopbackDestination: true,
		Resolver:                 resolver,
		UDPAssociateMode:         UDPAssociateModeDatagram,
	}
	serv, err := New(conf)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- serv.Serve(l)
	}()
	t.Cleanup(func() {
		serv.Close()
		l.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Errorf("socks5 server did not stop")
		}
	})
	return l.Addr().String()
}

func startTCPEchoServer(t *testing.T) *net.TCPAddr {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	t.Cleanup(func() {
		l.Close()
	})
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Errorf("io.ReadFull() failed: %v", err)
			return
		}
		if !bytes.Equal(buf, []byte("ping")) {
			t.Errorf("got %v, want %v", buf, []byte("ping"))
			return
		}
		if _, err := conn.Write([]byte("pong")); err != nil {
			t.Errorf("Write() failed: %v", err)
		}
	}()
	return l.Addr().(*net.TCPAddr)
}

func startUDPEchoServer(t *testing.T) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("net.ListenUDP() failed: %v", err)
	}
	t.Cleanup(func() {
		conn.Close()
	})
	go func() {
		buf := make([]byte, 1024)
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if !bytes.Equal(buf[:n], []byte("ping")) {
			t.Errorf("got %v, want %v", buf[:n], []byte("ping"))
			return
		}
		if _, err := conn.WriteToUDP([]byte("pong"), addr); err != nil {
			t.Errorf("WriteToUDP() failed: %v", err)
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr)
}

func startUDPRecordingServer(t *testing.T) (*net.UDPAddr, <-chan []byte) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("net.ListenUDP() failed: %v", err)
	}
	t.Cleanup(func() {
		conn.Close()
	})
	received := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 1024)
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		received <- append([]byte(nil), buf[:n]...)
	}()
	return conn.LocalAddr().(*net.UDPAddr), received
}

func dialUDPAssociate(t *testing.T, proxyAddr, targetAddr string) (net.Conn, *net.UDPConn, *net.UDPAddr) {
	t.Helper()
	dialer := DialSocks5Proxy(&Client{
		Host:    proxyAddr,
		CmdType: constant.Socks5UDPAssociateCmd,
	})
	ctrlConn, udpConn, proxyUDPAddr, err := dialer("udp", targetAddr)
	if err != nil {
		t.Fatalf("dial to socks5 proxy failed: %v", err)
	}
	return ctrlConn, udpConn, proxyUDPAddr
}

func transceiveUDPFQDNPacket(conn *net.UDPConn, proxyAddr *net.UDPAddr, dstHost string, dstPort int, payload []byte) ([]byte, error) {
	header := []byte{0, 0, 0, constant.Socks5FQDNAddress, byte(len(dstHost))}
	header = append(header, []byte(dstHost)...)
	header = append(header, byte(dstPort>>8), byte(dstPort))
	if _, err := conn.WriteToUDP(append(header, payload...), proxyAddr); err != nil {
		return nil, fmt.Errorf("WriteToUDP() failed: %w", err)
	}

	buf := make([]byte, 65536)
	n, readAddr, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, fmt.Errorf("ReadFromUDP() failed: %w", err)
	}
	if readAddr.Port != proxyAddr.Port {
		return nil, fmt.Errorf("unexpected read from a different address")
	}
	switch buf[3] {
	case constant.Socks5IPv4Address:
		if n <= 10 {
			return nil, fmt.Errorf("UDP associate response is too short for IPv4 address")
		}
		return buf[10:n], nil
	case constant.Socks5IPv6Address:
		if n <= 22 {
			return nil, fmt.Errorf("UDP associate response is too short for IPv6 address")
		}
		return buf[22:n], nil
	case constant.Socks5FQDNAddress:
		if n < 5 {
			return nil, fmt.Errorf("UDP associate response is too short for FQDN address")
		}
		headerLen := 7 + int(buf[4])
		if n <= headerLen {
			return nil, fmt.Errorf("UDP associate response is too short for FQDN address")
		}
		return buf[headerLen:n], nil
	default:
		return nil, fmt.Errorf("UDP associate unsupported address type: %d", buf[3])
	}
}
