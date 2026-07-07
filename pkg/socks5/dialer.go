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
	"context"
	"fmt"
	"io"
	"net"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/pool"
)

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

	conn, _, _, err := d.dial(ctx, constant.Socks5ConnectCmd, network, "", address)
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
	var remoteAddrSpec model.AddrSpec
	if err := remoteAddrSpec.From(raddr); err != nil {
		return nil, fmt.Errorf("parse remote address failed: %w", err)
	}
	remoteAddr := model.NetAddrSpec{
		AddrSpec: remoteAddrSpec,
		Net:      network,
	}

	ctrlConn, udpConn, serverListenAddr, err := d.dial(ctx, constant.Socks5UDPAssociateCmd, network, laddr, raddr)
	if err != nil {
		return nil, err
	}
	return &clientPacketConn{
		ctrlConn:         ctrlConn,
		udpConn:          udpConn,
		serverListenAddr: serverListenAddr,
		remoteAddr:       remoteAddr,
	}, nil
}

func (d *ClientDialer) dial(ctx context.Context, cmd byte, network, laddr, raddr string) (conn net.Conn, udpConn *net.UDPConn, serverListenAddr *net.UDPAddr, err error) {
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
			if conn != nil {
				conn.Close()
			}
			if udpConn != nil {
				udpConn.Close()
			}
		}
	}()
	stopCancelWatcher := setConnDeadlineOnContextDone(ctx, conn)
	defer stopCancelWatcher()

	if err = clientNegotiateAuthentication(conn, d.Credential); err != nil {
		return nil, nil, nil, err
	}

	var dstAddr model.AddrSpec
	if err = dstAddr.From(raddr); err != nil {
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
		// Parse socks5 proxy server's bind address from response.
		serverListenAddr, err = socks5UDPAddrFromResponse(conn, resp)
		if err != nil {
			return nil, nil, nil, err
		}

		// Create local UDP listener for communicating with the socks5 proxy server.
		udpConn, err = net.ListenUDP("udp4", nil)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return conn, udpConn, serverListenAddr, nil
}

type clientPacketConn struct {
	ctrlConn         net.Conn
	udpConn          *net.UDPConn
	serverListenAddr *net.UDPAddr
	remoteAddr       net.Addr // destination address, not proxy server address
}

var _ net.PacketConn = (*clientPacketConn)(nil)

func (c *clientPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	buf := pool.GetBuf64k()
	defer pool.PutBuf64k(buf)
	for i := 0; i < 65536; i++ {
		c.udpConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, addr, err := c.udpConn.ReadFromUDP(buf)
		if err != nil {
			return 0, nil, err
		}
		if c.serverListenAddr == nil || !sameUDPAddr(addr, c.serverListenAddr) {
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
	return 0, nil, io.ErrNoProgress
}

func (c *clientPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if addr == nil {
		addr = c.remoteAddr
	}
	pkt, err := wrapSocks5UDPPacket(addr, p)
	if err != nil {
		return 0, err
	}
	if _, err := c.udpConn.WriteToUDP(pkt, c.serverListenAddr); err != nil {
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
