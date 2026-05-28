package socks5

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

// handleRequest is used for request processing after authentication.
func (s *Server) handleRequest(ctx context.Context, req *model.Request, conn net.Conn) error {
	// Resolve the address if we have a FQDN.
	dst := &req.DstAddr
	if dst.FQDN != "" {
		ips, err := s.config.Resolver.LookupIP(ctx, "ip", dst.FQDN)
		if err != nil || len(ips) == 0 {
			DNSResolveErrors.Add(1)
			if err := model.WriteSocks5Response(conn, constant.Socks5ReplyHostUnreachable, zeroBindAddr()); err != nil {
				return fmt.Errorf("failed to send reply: %w", err)
			}
			if err != nil {
				return fmt.Errorf("failed to resolve destination %q: %w", dst.FQDN, err)
			} else {
				return fmt.Errorf(stderror.IPAddressNotFound, dst.FQDN)
			}
		} else {
			dst.IP = common.SelectIPFromList(ips, s.config.DualStackPreference)
			if dst.IP == nil {
				DNSResolveErrors.Add(1)
				if err := model.WriteSocks5Response(conn, constant.Socks5ReplyNetworkUnreachable, zeroBindAddr()); err != nil {
					return fmt.Errorf("failed to send reply: %w", err)
				}
				return fmt.Errorf("resolved domain name %s to IP addresses %v, but no IP address satisfy DNS dual stack preference", dst.FQDN, ips)
			}
			log.Debugf("Resolved domain name %s to IP addresses %v, selected IP address %v", dst.FQDN, ips, dst.IP)
		}
	}

	// Switch on the command.
	switch req.Command {
	case constant.Socks5ConnectCmd:
		return s.handleConnect(ctx, req, conn)
	case constant.Socks5BindCmd:
		return s.handleBind(ctx, req, conn)
	case constant.Socks5UDPAssociateCmd:
		return s.handleAssociate(ctx, req, conn)
	default:
		UnsupportedCommandErrors.Add(1)
		if err := model.WriteSocks5Response(conn, constant.Socks5ReplyCommandNotSupported, zeroBindAddr()); err != nil {
			return fmt.Errorf("failed to send reply: %w", err)
		}
		return fmt.Errorf("unsupported command: %v", req.Command)
	}
}

// handleConnect is used to handle a connect command.
func (s *Server) handleConnect(ctx context.Context, req *model.Request, conn net.Conn) error {
	var d net.Dialer
	target, err := d.DialContext(ctx, "tcp", req.DstAddr.String())
	if err != nil {
		msg := err.Error()
		var replyCode uint8
		if strings.Contains(msg, "refused") {
			replyCode = constant.Socks5ReplyConnectionRefused
			ConnectionRefusedErrors.Add(1)
		} else if strings.Contains(msg, "network is unreachable") {
			replyCode = constant.Socks5ReplyNetworkUnreachable
			NetworkUnreachableErrors.Add(1)
		} else {
			replyCode = constant.Socks5ReplyHostUnreachable
			HostUnreachableErrors.Add(1)
		}
		if err := model.WriteSocks5Response(conn, replyCode, zeroBindAddr()); err != nil {
			return fmt.Errorf("failed to send reply: %w", err)
		}
		return fmt.Errorf("connect to %v failed: %w", req.DstAddr, err)
	}
	defer target.Close()

	// Send success.
	local := target.LocalAddr().(*net.TCPAddr)
	bind := model.AddrSpec{IP: local.IP, Port: local.Port}
	if err := model.WriteSocks5Response(conn, constant.Socks5ReplySuccess, bind); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("failed to send reply: %w", err)
	}

	return common.BidiCopy(conn, target)
}

// handleBind is used to handle a bind command.
func (s *Server) handleBind(_ context.Context, _ *model.Request, conn net.Conn) error {
	UnsupportedCommandErrors.Add(1)
	if err := model.WriteSocks5Response(conn, constant.Socks5ReplyCommandNotSupported, zeroBindAddr()); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("failed to send reply: %w", err)
	}
	return nil
}

// handleAssociate is used to handle an associate command.
func (s *Server) handleAssociate(ctx context.Context, req *model.Request, conn net.Conn) error {
	if s.config.UDPAssociateMode == UDPAssociateModeDatagram {
		return s.handleAssociateDatagram(ctx, req, conn)
	}
	return s.handleAssociatePacketOverStream(ctx, req, conn)
}

func (s *Server) handleAssociatePacketOverStream(ctx context.Context, _ *model.Request, conn net.Conn) error {
	// Create a UDP listener on a random port.
	// All the requests associated to this connection will go through this port.
	udpListenerAddr, err := apicommon.ResolveUDPAddr(ctx, s.config.Resolver, "udp", common.MaybeDecorateIPv6(common.AllIPAddr())+":0")
	if err != nil {
		UDPAssociateErrors.Add(1)
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}
	udpConn, err := net.ListenUDP("udp", udpListenerAddr)
	if err != nil {
		UDPAssociateErrors.Add(1)
		return fmt.Errorf("failed to listen UDP: %w", err)
	}

	// Use 0.0.0.0:<port> as the bind address.
	// This is the port used by the server. Client will rewrite the port number.
	// As the traffic between the client and the server goes through tunnel,
	// it is OK to use an IPv4 bind address even though the UDP listener is IPv6.
	udpPort, err := localUDPPort(udpConn)
	if err != nil {
		UDPAssociateErrors.Add(1)
		return err
	}
	bind := model.AddrSpec{IP: net.IP{0, 0, 0, 0}, Port: udpPort}
	if err := model.WriteSocks5Response(conn, constant.Socks5ReplySuccess, bind); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("failed to send reply: %w", err)
	}

	return RunUDPAssociateLoop(udpConn, apicommon.NewPacketOverStreamTunnel(conn), s.config.Resolver)
}

func (s *Server) handleAssociateDatagram(ctx context.Context, _ *model.Request, conn net.Conn) error {
	// Create a UDP listener on a random port.
	// All the requests associated to this connection will go through this port.
	udpListenerAddr, err := apicommon.ResolveUDPAddr(ctx, s.config.Resolver, "udp", common.MaybeDecorateIPv6(common.AllIPAddr())+":0")
	if err != nil {
		UDPAssociateErrors.Add(1)
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}
	udpConn, err := net.ListenUDP("udp", udpListenerAddr)
	if err != nil {
		UDPAssociateErrors.Add(1)
		return fmt.Errorf("failed to listen UDP: %w", err)
	}

	local, ok := udpConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		udpConn.Close()
		UDPAssociateErrors.Add(1)
		return fmt.Errorf("UDP listener local address has unexpected type %T", udpConn.LocalAddr())
	}
	bind := model.AddrSpec{IP: socks5UDPAssociateBindIP(conn, local), Port: local.Port}
	if err := model.WriteSocks5Response(conn, constant.Socks5ReplySuccess, bind); err != nil {
		udpConn.Close()
		HandshakeErrors.Add(1)
		return fmt.Errorf("failed to send reply: %w", err)
	}

	return runUDPAssociateDatagramLoop(udpConn, conn, s.config.Resolver)
}

func socks5UDPAssociateBindIP(conn net.Conn, udpLocal *net.UDPAddr) net.IP {
	if tcpAddr, ok := conn.LocalAddr().(*net.TCPAddr); ok && tcpAddr.IP != nil && !tcpAddr.IP.IsUnspecified() {
		return tcpAddr.IP
	}
	if udpLocal != nil && udpLocal.IP != nil && !udpLocal.IP.IsUnspecified() {
		return udpLocal.IP
	}
	return net.IPv4(0, 0, 0, 0)
}

func (s *Server) handleForwarding(req *model.Request, conn net.Conn, proxy *appctlpb.EgressProxy) error {
	forwardHost := proxy.GetHost()
	forwardPort := proxy.GetPort()
	proxyConn, err := net.Dial("tcp", common.MaybeDecorateIPv6(forwardHost)+":"+strconv.Itoa(int(forwardPort)))
	if err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("dial to egress proxy failed: %w", err)
	}

	if err := s.dialWithAuthentication(proxyConn, proxy.GetSocks5Authentication()); err != nil {
		return err
	}

	// Handle based on command type
	switch req.Command {
	case constant.Socks5ConnectCmd:
		if _, err := proxyConn.Write(req.Raw); err != nil {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return fmt.Errorf("failed to write socks5 request to egress proxy: %w", err)
		}
		return common.BidiCopy(conn, proxyConn)
	case constant.Socks5UDPAssociateCmd:
		return s.handleForwardingUDP(req, conn, proxyConn)
	default:
		return fmt.Errorf("unsupported command: %v", req.Command)
	}
}

func (s *Server) handleForwardingUDP(req *model.Request, conn net.Conn, proxyConn net.Conn) error {
	// Write UDP associate request to downstream server.
	if _, err := proxyConn.Write(req.Raw); err != nil {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		return fmt.Errorf("failed to write socks5 request to egress proxy: %w", err)
	}

	resp, err := model.ReadSocks5Response(proxyConn)
	if err != nil {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		return fmt.Errorf("failed to read socks5 response from egress proxy: %w", err)
	}

	// Check if response indicates success, and forward the error response to client.
	if resp.Reply != constant.Socks5ReplySuccess {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		conn.Write(resp.Raw)
		return fmt.Errorf("egress proxy returned error: %d", resp.Reply)
	}

	// Parse socks5 proxy server's bind address from response.
	downstreamUDPAddr, err := socks5UDPAddrFromResponse(proxyConn, resp)
	if err != nil {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		return err
	}

	// Create local UDP listener for communicating with the socks5 proxy server.
	udpConn, err := listenUDP4()
	if err != nil {
		UDPAssociateErrors.Add(1)
		proxyConn.Close()
		return fmt.Errorf("failed to listen UDP: %w", err)
	}

	// Rewrite response port with local UDP port.
	udpPort, err := localUDPPort(udpConn)
	if err != nil {
		UDPAssociateErrors.Add(1)
		udpConn.Close()
		proxyConn.Close()
		return err
	}
	if err := rewriteSocks5ResponseBindPort(resp, udpPort); err != nil {
		UDPAssociateErrors.Add(1)
		udpConn.Close()
		proxyConn.Close()
		return err
	}

	// Send response to client.
	if _, err := conn.Write(resp.Raw); err != nil {
		UDPAssociateErrors.Add(1)
		udpConn.Close()
		proxyConn.Close()
		return fmt.Errorf("failed to send socks5 response to client: %w", err)
	}

	clientTunnel := apicommon.NewPacketOverStreamTunnel(conn)
	return RunUDPForwardingLoop(udpConn, clientTunnel, downstreamUDPAddr, proxyConn)
}

// proxySocks5AuthReq transfers the socks5 authentication request and response
// between socks5 client and server.
func (s *Server) proxySocks5AuthReq(conn, proxyConn net.Conn) error {
	// Send the version and authtication methods to the server.
	defer common.SetReadTimeout(conn, 0)
	defer common.SetReadTimeout(proxyConn, 0)
	common.SetReadTimeout(conn, s.config.HandshakeTimeout)
	version := []byte{0}
	if _, err := io.ReadFull(conn, version); err != nil {
		return fmt.Errorf("failed to get version byte: %w", err)
	}
	if version[0] != constant.Socks5Version {
		return fmt.Errorf("unsupported SOCKS version: %v", version)
	}
	nMethods := []byte{0}
	if _, err := io.ReadFull(conn, nMethods); err != nil {
		return fmt.Errorf("failed to get the length of authentication methods: %w", err)
	}
	methods := make([]byte, int(nMethods[0]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return fmt.Errorf("failed to get authentication methods: %w", err)
	}
	authReq := []byte{}
	authReq = append(authReq, version...)
	authReq = append(authReq, nMethods...)
	authReq = append(authReq, methods...)
	if _, err := proxyConn.Write(authReq); err != nil {
		return fmt.Errorf("failed to write authentication request to the server: %w", err)
	}

	// Get server authentication response.
	common.SetReadTimeout(proxyConn, s.config.HandshakeTimeout)
	authResp := make([]byte, 2)
	if _, err := io.ReadFull(proxyConn, authResp); err != nil {
		return fmt.Errorf("failed to read authentication response from the socks5 server: %w", err)
	}
	if _, err := conn.Write(authResp); err != nil {
		return fmt.Errorf("failed to write authentication response to the socks5 client: %w", err)
	}

	return nil
}

// proxySocks5ConnReq transfers the socks5 connection request and response
// between socks5 client and server.
// If HandshakeNoWait is true, socks5 client will fake the connection response
// and return the pending connection request to the server.
// If UDP association is used, return the created UDP connection.
func (s *Server) proxySocks5ConnReq(conn, proxyConn net.Conn) (*net.UDPConn, []byte, error) {
	defer common.SetReadTimeout(conn, 0)
	defer common.SetReadTimeout(proxyConn, 0)
	common.SetReadTimeout(conn, s.config.HandshakeTimeout)

	req, err := model.ReadSocks5Request(conn)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get socks5 connection request: %w", err)
	}
	cmd := req.Command
	connReq := req.Raw

	// Fake server connection response if HandshakeNoWait is true.
	if s.config.HandshakeNoWait && cmd == constant.Socks5ConnectCmd {
		var resp bytes.Buffer
		// Instead of using server bound address and port, use the proxy connection address and port.
		serverBoundAddr := model.NetAddrSpec{}
		if err := serverBoundAddr.From(proxyConn.RemoteAddr()); err != nil {
			return nil, nil, fmt.Errorf("failed to get proxy connection address: %w", err)
		}
		resp.Write([]byte{constant.Socks5Version, constant.Socks5ReplySuccess, 0})
		if err := serverBoundAddr.WriteToSocks5(&resp); err != nil {
			return nil, nil, fmt.Errorf("failed to write proxy connection address: %w", err)
		}

		if _, err := conn.Write(resp.Bytes()); err != nil {
			return nil, nil, fmt.Errorf("failed to write fake connection response to the socks5 client: %w", err)
		}
		return nil, connReq, nil
	}

	// Send the connection request to the server.
	if _, err := proxyConn.Write(connReq); err != nil {
		return nil, nil, fmt.Errorf("failed to write connection request to the server: %w", err)
	}
	log.Debugf("Sent socks5 request %v to server", connReq)

	// Get server connection response.
	common.SetReadTimeout(proxyConn, s.config.HandshakeTimeout)
	connResp, err := model.ReadSocks5Response(proxyConn)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read connection response from the server: %w", err)
	}
	log.Debugf("Received socks5 response %v from server", connResp.Raw)

	// Handle UDP association.
	var udpConn *net.UDPConn
	if cmd == constant.Socks5UDPAssociateCmd {
		// Create a UDP listener on a random port in IPv4 network.
		// Assume server uses 0.0.0.0:<port> as the bind address so we only need to change port number.
		udpConn, err = listenUDP4()
		if err != nil {
			return nil, nil, fmt.Errorf("net.ListenUDP() failed: %w", err)
		}
		// Get the port number and rewrite the response.
		udpPort, err := localUDPPort(udpConn)
		if err != nil {
			udpConn.Close()
			return nil, nil, err
		}
		if err := rewriteSocks5ResponseBindPort(connResp, udpPort); err != nil {
			udpConn.Close()
			return nil, nil, err
		}
	}

	if _, err := conn.Write(connResp.Raw); err != nil {
		return nil, nil, fmt.Errorf("failed to write connection response to the socks5 client: %w", err)
	}
	return udpConn, nil, nil
}

// readRequest creates a new Request from the connection.
func (s *Server) readRequest(conn io.Reader) (*model.Request, error) {
	if netConn, ok := conn.(net.Conn); ok {
		common.SetReadTimeout(netConn, s.config.HandshakeTimeout)
		defer common.SetReadTimeout(netConn, 0)
	}

	req := &model.Request{}
	if err := req.ReadFromSocks5(conn); err != nil {
		return nil, err
	}
	return req, nil
}
