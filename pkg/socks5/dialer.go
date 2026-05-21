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
	"strconv"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
)

const socks5UDPAssociateRequestAddr = "0.0.0.0:0"

// ClientDialer connects to upstream endpoints through a socks5 proxy.
type ClientDialer struct {
	ProxyAddress       string
	Credential         *Credential
	Timeout            time.Duration
	Socks5UDPAssociate bool
}

var _ apicommon.Dialer = (*ClientDialer)(nil)
var _ apicommon.PacketDialer = (*ClientDialer)(nil)

// NewClientDialer creates a socks5 client dialer.
func NewClientDialer(proxyAddress string, credential *Credential, socks5UDPAssociate bool) *ClientDialer {
	return &ClientDialer{
		ProxyAddress:       proxyAddress,
		Credential:         credential,
		Socks5UDPAssociate: socks5UDPAssociate,
	}
}

// DialContext creates a stream connection through socks5 CONNECT.
func (d *ClientDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, net.UnknownNetworkError(network)
	}

	conn, _, _, err := d.dial(ctx, constant.Socks5ConnectCmd, network, address, "")
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// ListenPacket creates a packet connection through socks5 UDP ASSOCIATE.
func (d *ClientDialer) ListenPacket(ctx context.Context, network, laddr, raddr string) (net.PacketConn, error) {
	switch network {
	case "udp", "udp4", "udp6":
	default:
		return nil, net.UnknownNetworkError(network)
	}
	if !d.Socks5UDPAssociate {
		return nil, fmt.Errorf("socks5 UDP ASSOCIATE is disabled")
	}

	remoteAddr, err := parseNetAddr(network, raddr)
	if err != nil {
		return nil, fmt.Errorf("parse remote address failed: %w", err)
	}
	ctrlConn, udpConn, proxyUDPAddr, err := d.dial(ctx, constant.Socks5UDPAssociateCmd, network, socks5UDPAssociateRequestAddr, laddr)
	if err != nil {
		return nil, err
	}
	return &clientPacketConn{
		ctrlConn:     ctrlConn,
		udpConn:      udpConn,
		proxyUDPAddr: proxyUDPAddr,
		remoteAddr:   remoteAddr,
	}, nil
}

func (d *ClientDialer) dial(ctx context.Context, cmd byte, network, targetAddr, laddr string) (conn net.Conn, udpConn *net.UDPConn, proxyUDPAddr *net.UDPAddr, err error) {
	if d == nil {
		return nil, nil, nil, fmt.Errorf("socks5 client dialer is nil")
	}
	if d.ProxyAddress == "" {
		return nil, nil, nil, fmt.Errorf("socks5 proxy address is empty")
	}
	if cmd != constant.Socks5ConnectCmd && cmd != constant.Socks5UDPAssociateCmd {
		return nil, nil, nil, fmt.Errorf("socks5 command %d is not supported", cmd)
	}
	if d.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}

	rawDialer := &net.Dialer{}
	conn, err = rawDialer.DialContext(ctx, "tcp", d.ProxyAddress)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("dial socks5 proxy failed: %w", err)
	}
	defer func() {
		if err != nil {
			conn.Close()
			if udpConn != nil {
				udpConn.Close()
			}
		}
	}()
	stopCancelWatcher := setConnDeadlineOnContextDone(ctx, conn)
	defer stopCancelWatcher()

	if err = d.negotiateAuthentication(conn); err != nil {
		return nil, nil, nil, err
	}

	dstAddr, err := parseAddrSpec(targetAddr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse target address failed: %w", err)
	}
	req := &model.Request{
		Command: cmd,
		DstAddr: dstAddr,
	}
	if err = req.WriteToSocks5(conn); err != nil {
		return nil, nil, nil, fmt.Errorf("write socks5 request failed: %w", err)
	}

	resp := &model.Response{}
	if err = resp.ReadFromSocks5(conn); err != nil {
		return nil, nil, nil, fmt.Errorf("read socks5 response failed: %w", err)
	}
	if resp.Reply != constant.Socks5ReplySuccess {
		return nil, nil, nil, fmt.Errorf("socks5 request failed with reply code %d", resp.Reply)
	}
	if cmd == constant.Socks5UDPAssociateCmd {
		proxyUDPAddr, err = socks5UDPAddrFromResponse(conn, resp)
		if err != nil {
			return nil, nil, nil, err
		}
		udpConn, err = listenUDPForSocks5(network, laddr, proxyUDPAddr)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return conn, udpConn, proxyUDPAddr, nil
}

func setConnDeadlineOnContextDone(ctx context.Context, conn net.Conn) func() {
	done := ctx.Done()
	if done == nil {
		return func() {}
	}

	stop := make(chan struct{})
	go func() {
		select {
		case <-done:
			conn.SetDeadline(time.Now())
		case <-stop:
		}
	}()
	return func() {
		close(stop)
	}
}

func (d *ClientDialer) negotiateAuthentication(conn net.Conn) error {
	method := constant.Socks5NoAuth
	if d.Credential != nil {
		method = constant.Socks5UserPassAuth
	}
	if _, err := conn.Write([]byte{constant.Socks5Version, 1, method}); err != nil {
		return fmt.Errorf("write socks5 authentication methods failed: %w", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("read socks5 authentication method failed: %w", err)
	}
	if resp[0] != constant.Socks5Version || resp[1] != method {
		return fmt.Errorf("socks5 authentication method negotiation failed: %v", resp)
	}
	if d.Credential == nil {
		return nil
	}

	if len(d.Credential.User) > 255 || len(d.Credential.Password) > 255 {
		return fmt.Errorf("socks5 username and password must not exceed 255 bytes")
	}
	var req bytes.Buffer
	req.WriteByte(constant.Socks5UserPassAuthVersion)
	req.WriteByte(byte(len(d.Credential.User)))
	req.WriteString(d.Credential.User)
	req.WriteByte(byte(len(d.Credential.Password)))
	req.WriteString(d.Credential.Password)
	if _, err := conn.Write(req.Bytes()); err != nil {
		return fmt.Errorf("write socks5 username password authentication failed: %w", err)
	}
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("read socks5 username password authentication failed: %w", err)
	}
	if resp[0] != constant.Socks5UserPassAuthVersion || resp[1] != constant.Socks5AuthSuccess {
		return fmt.Errorf("socks5 username password authentication failed: %v", resp)
	}
	return nil
}

type clientPacketConn struct {
	ctrlConn     net.Conn
	udpConn      *net.UDPConn
	proxyUDPAddr *net.UDPAddr
	remoteAddr   net.Addr
}

var _ net.PacketConn = (*clientPacketConn)(nil)

func (c *clientPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	buf := make([]byte, 65536)
	for {
		n, addr, err := c.udpConn.ReadFromUDP(buf)
		if err != nil {
			return 0, nil, err
		}
		if !sameUDPAddrPort(addr, c.proxyUDPAddr) {
			continue
		}
		payload, err := unwrapSocks5UDPPacket(buf[:n])
		if err != nil {
			return 0, nil, err
		}
		if len(payload) > len(p) {
			return 0, nil, io.ErrShortBuffer
		}
		copy(p, payload)
		return len(payload), c.remoteAddr, nil
	}
}

func (c *clientPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if addr == nil {
		addr = c.remoteAddr
	}
	pkt, err := wrapSocks5UDPPacket(addr, p)
	if err != nil {
		return 0, err
	}
	if _, err := c.udpConn.WriteToUDP(pkt, c.proxyUDPAddr); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *clientPacketConn) Close() error {
	udpErr := c.udpConn.Close()
	ctrlErr := c.ctrlConn.Close()
	if udpErr != nil {
		return udpErr
	}
	return ctrlErr
}

func (c *clientPacketConn) LocalAddr() net.Addr {
	return c.udpConn.LocalAddr()
}

func (c *clientPacketConn) SetDeadline(t time.Time) error {
	return c.udpConn.SetDeadline(t)
}

func (c *clientPacketConn) SetReadDeadline(t time.Time) error {
	return c.udpConn.SetReadDeadline(t)
}

func (c *clientPacketConn) SetWriteDeadline(t time.Time) error {
	return c.udpConn.SetWriteDeadline(t)
}

func parseNetAddr(network, address string) (net.Addr, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("port %d is out of range", port)
	}
	if ip := net.ParseIP(host); ip != nil {
		if network == "tcp" || network == "tcp4" || network == "tcp6" {
			return &net.TCPAddr{IP: ip, Port: port}, nil
		}
		return &net.UDPAddr{IP: ip, Port: port}, nil
	}
	if len(host) > 255 {
		return nil, fmt.Errorf("FQDN %q exceeds 255 bytes", host)
	}
	return &model.NetAddrSpec{
		Net: network,
		AddrSpec: model.AddrSpec{
			FQDN: host,
			Port: port,
		},
	}, nil
}

func parseAddrSpec(address string) (model.AddrSpec, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return model.AddrSpec{}, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return model.AddrSpec{}, err
	}
	if port < 0 || port > 65535 {
		return model.AddrSpec{}, fmt.Errorf("port %d is out of range", port)
	}
	if ip := net.ParseIP(host); ip != nil {
		return model.AddrSpec{IP: ip, Port: port}, nil
	}
	if len(host) > 255 {
		return model.AddrSpec{}, fmt.Errorf("FQDN %q exceeds 255 bytes", host)
	}
	return model.AddrSpec{FQDN: host, Port: port}, nil
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

func listenUDPForSocks5(network, laddr string, proxyUDPAddr *net.UDPAddr) (*net.UDPConn, error) {
	listenNetwork := network
	if listenNetwork == "udp" {
		if proxyUDPAddr.IP.To4() != nil {
			listenNetwork = "udp4"
		} else {
			listenNetwork = "udp6"
		}
	}
	var localUDPAddr *net.UDPAddr
	if laddr != "" {
		var err error
		localUDPAddr, err = net.ResolveUDPAddr(listenNetwork, laddr)
		if err != nil {
			return nil, fmt.Errorf("net.ResolveUDPAddr() failed: %w", err)
		}
	}
	conn, err := net.ListenUDP(listenNetwork, localUDPAddr)
	if err != nil {
		return nil, fmt.Errorf("net.ListenUDP() failed: %w", err)
	}
	return conn, nil
}

func wrapSocks5UDPPacket(addr net.Addr, payload []byte) ([]byte, error) {
	var netAddr model.NetAddrSpec
	if err := netAddr.From(addr); err != nil {
		return nil, err
	}
	if len(netAddr.FQDN) > 255 {
		return nil, fmt.Errorf("FQDN %q exceeds 255 bytes", netAddr.FQDN)
	}
	var buf bytes.Buffer
	buf.Write([]byte{0, 0, 0})
	if err := netAddr.AddrSpec.WriteToSocks5(&buf); err != nil {
		return nil, err
	}
	buf.Write(payload)
	return buf.Bytes(), nil
}

func unwrapSocks5UDPPacket(pkt []byte) ([]byte, error) {
	if len(pkt) <= 6 {
		return nil, fmt.Errorf("socks5 UDP packet is too short")
	}
	if pkt[0] != 0 || pkt[1] != 0 {
		return nil, fmt.Errorf("socks5 UDP reserved bytes are invalid")
	}
	if pkt[2] != 0 {
		return nil, fmt.Errorf("socks5 UDP fragmentation is not supported")
	}
	r := bytes.NewReader(pkt[3:])
	var addr model.AddrSpec
	if err := addr.ReadFromSocks5(r); err != nil {
		return nil, err
	}
	return pkt[len(pkt)-r.Len():], nil
}

func sameUDPAddrPort(a, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Port == b.Port
}
