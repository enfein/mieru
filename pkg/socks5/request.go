package socks5

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

// socks5 error types.
const (
	successReply         byte = 0
	serverFailure        byte = 1
	notAllowedByRuleSet  byte = 2
	networkUnreachable   byte = 3
	hostUnreachable      byte = 4
	connectionRefused    byte = 5
	ttlExpired           byte = 6
	commandNotSupported  byte = 7
	addrTypeNotSupported byte = 8
)

// A Request represents request received by a server.
type Request struct {
	// Protocol version.
	Version uint8
	// Requested command.
	Command uint8
	// AddrSpec of the desired destination.
	DstAddr *model.AddrSpec
	// Raw request bytes.
	Raw []byte
}

// newRequest creates a new Request from the connection.
func (s *Server) newRequest(conn io.Reader) (*Request, error) {
	// Read the version byte.
	header := []byte{0, 0, 0}
	if netConn, ok := conn.(net.Conn); ok {
		common.SetReadTimeout(netConn, s.config.HandshakeTimeout)
		defer common.SetReadTimeout(netConn, 0)
	}
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("failed to get command version: %w", err)
	}

	// Ensure we are compatible.
	if header[0] != constant.Socks5Version {
		return nil, fmt.Errorf("unsupported command version: %v", header[0])
	}

	// Read in the destination address.
	dst := &model.AddrSpec{}
	if err := dst.ReadFromSocks5(conn); err != nil {
		return nil, err
	}
	var dstBuf bytes.Buffer
	if err := dst.WriteToSocks5(&dstBuf); err != nil {
		return nil, err
	}

	return &Request{
		Version: constant.Socks5Version,
		Command: header[1],
		DstAddr: dst,
		Raw:     append(header, dstBuf.Bytes()...),
	}, nil
}

// handleRequest is used for request processing after authentication.
func (s *Server) handleRequest(ctx context.Context, req *Request, conn net.Conn) error {
	// Resolve the address if we have a FQDN.
	dst := req.DstAddr
	if dst.FQDN != "" {
		ips, err := s.config.Resolver.LookupIP(ctx, "ip", dst.FQDN)
		if err != nil || len(ips) == 0 {
			DNSResolveErrors.Add(1)
			if err := sendReply(conn, hostUnreachable, nil); err != nil {
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
				if err := sendReply(conn, networkUnreachable, nil); err != nil {
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
		if err := sendReply(conn, commandNotSupported, nil); err != nil {
			return fmt.Errorf("failed to send reply: %w", err)
		}
		return fmt.Errorf("unsupported command: %v", req.Command)
	}
}

// handleConnect is used to handle a connect command.
func (s *Server) handleConnect(ctx context.Context, req *Request, conn net.Conn) error {
	var d net.Dialer
	target, err := d.DialContext(ctx, "tcp", req.DstAddr.String())
	if err != nil {
		msg := err.Error()
		var resp uint8
		if strings.Contains(msg, "refused") {
			resp = connectionRefused
			ConnectionRefusedErrors.Add(1)
		} else if strings.Contains(msg, "network is unreachable") {
			resp = networkUnreachable
			NetworkUnreachableErrors.Add(1)
		} else {
			resp = hostUnreachable
			HostUnreachableErrors.Add(1)
		}
		if err := sendReply(conn, resp, nil); err != nil {
			return fmt.Errorf("failed to send reply: %w", err)
		}
		return fmt.Errorf("connect to %v failed: %w", req.DstAddr, err)
	}
	defer target.Close()

	// Send success.
	local := target.LocalAddr().(*net.TCPAddr)
	bind := model.AddrSpec{IP: local.IP, Port: local.Port}
	if err := sendReply(conn, successReply, &bind); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("failed to send reply: %w", err)
	}

	return common.BidiCopy(conn, target)
}

// handleBind is used to handle a bind command.
func (s *Server) handleBind(_ context.Context, _ *Request, conn net.Conn) error {
	UnsupportedCommandErrors.Add(1)
	if err := sendReply(conn, commandNotSupported, nil); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("failed to send reply: %w", err)
	}
	return nil
}

// handleAssociate is used to handle a associate command.
func (s *Server) handleAssociate(_ context.Context, _ *Request, conn net.Conn) error {
	// Create a UDP listener on a random port.
	// All the requests associated to this connection will go through this port.
	udpListenerAddr, err := apicommon.ResolveUDPAddr(s.config.Resolver, "udp", common.MaybeDecorateIPv6(common.AllIPAddr())+":0")
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
	_, udpPortStr, err := net.SplitHostPort(udpConn.LocalAddr().String())
	if err != nil {
		UDPAssociateErrors.Add(1)
		return fmt.Errorf("net.SplitHostPort() failed: %w", err)
	}
	udpPort, err := strconv.Atoi(udpPortStr)
	if err != nil {
		UDPAssociateErrors.Add(1)
		return fmt.Errorf("strconv.Atoi() failed: %w", err)
	}
	bind := model.AddrSpec{IP: net.IP{0, 0, 0, 0}, Port: udpPort}
	if err := sendReply(conn, successReply, &bind); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("failed to send reply: %w", err)
	}

	conn = apicommon.NewPacketOverStreamTunnel(conn)
	var udpErr atomic.Value

	// addrMap maps the UDPAddr in string to the bytes in UDP associate header.
	var addrMap sync.Map

	var wg sync.WaitGroup
	wg.Add(2)

	// Send UDP packets to destinations.
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
			if addrType != 0x01 && addrType != 0x03 && addrType != 0x04 {
				udpErr.Store(stderror.ErrInvalidArgument)
				UDPAssociateErrors.Add(1)
				return
			}
			if (addrType == 0x01 && n <= 10) || (addrType == 0x03 && n <= int(buf[4])+6) || (addrType == 0x04 && n <= 22) {
				udpErr.Store(stderror.ErrNoEnoughData)
				UDPAssociateErrors.Add(1)
				return
			}

			// Get target address and send data.
			switch addrType {
			case 0x01:
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
			case 0x03:
				fqdnLen := buf[4]
				fqdn := string(buf[5 : 5+fqdnLen])
				dstAddr, err := apicommon.ResolveUDPAddr(s.config.Resolver, "udp", fqdn+":"+strconv.Itoa(int(buf[5+fqdnLen])<<8+int(buf[6+fqdnLen])))
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
			case 0x04:
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

	// Receive UDP packets from destinations.
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

func (s *Server) handleForwarding(req *Request, conn net.Conn, proxy *appctlpb.EgressProxy) error {
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

	if _, err := proxyConn.Write(req.Raw); err != nil {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		return fmt.Errorf("failed to write socks5 request to egress proxy: %w", err)
	}
	return common.BidiCopy(conn, proxyConn)
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
// If HandshakeNoWait is true, socks5 client will fake the connection response.
// If UDP association is used, return the created UDP connection, otherwise return nil.
func (s *Server) proxySocks5ConnReq(conn, proxyConn net.Conn) (*net.UDPConn, error) {
	// Send the connection request to the server.
	defer common.SetReadTimeout(conn, 0)
	defer common.SetReadTimeout(proxyConn, 0)
	common.SetReadTimeout(conn, s.config.HandshakeTimeout)
	connReq := make([]byte, 4)
	if _, err := io.ReadFull(conn, connReq); err != nil {
		return nil, fmt.Errorf("failed to get socks5 connection request: %w", err)
	}
	cmd := connReq[1]
	reqAddrType := connReq[3]
	var reqFQDNLen []byte
	var dstAddr []byte
	switch reqAddrType {
	case constant.Socks5IPv4Address:
		dstAddr = make([]byte, 6)
	case constant.Socks5FQDNAddress:
		reqFQDNLen = []byte{0}
		if _, err := io.ReadFull(conn, reqFQDNLen); err != nil {
			return nil, fmt.Errorf("failed to get FQDN length: %w", err)
		}
		dstAddr = make([]byte, reqFQDNLen[0]+2)
	case constant.Socks5IPv6Address:
		dstAddr = make([]byte, 18)
	default:
		return nil, fmt.Errorf("unsupported address type: %d", reqAddrType)
	}
	if _, err := io.ReadFull(conn, dstAddr); err != nil {
		return nil, fmt.Errorf("failed to get destination address: %w", err)
	}
	if len(reqFQDNLen) != 0 {
		connReq = append(connReq, reqFQDNLen...)
	}
	connReq = append(connReq, dstAddr...)
	if _, err := proxyConn.Write(connReq); err != nil {
		return nil, fmt.Errorf("failed to write connection request to the server: %w", err)
	}
	log.Debugf("Sent socks5 request %v to server", connReq)

	// Fake server connection response if HandshakeNoWait is true.
	if s.config.HandshakeNoWait && cmd == constant.Socks5ConnectCmd {
		var resp bytes.Buffer
		// Instead of using server bound address and port, use the proxy connection address and port.
		serverBoundAddr := model.NetAddrSpec{}
		if err := serverBoundAddr.From(proxyConn.RemoteAddr()); err != nil {
			return nil, fmt.Errorf("failed to get proxy connection address: %w", err)
		}
		resp.Write([]byte{constant.Socks5Version, 0, 0})
		if err := serverBoundAddr.WriteToSocks5(&resp); err != nil {
			return nil, fmt.Errorf("failed to write proxy connection address: %w", err)
		}

		if _, err := conn.Write(resp.Bytes()); err != nil {
			return nil, fmt.Errorf("failed to write fake connection response to the socks5 client: %w", err)
		}
		return nil, nil
	}

	// Get server connection response.
	common.SetReadTimeout(proxyConn, s.config.HandshakeTimeout)
	connResp := make([]byte, 4)
	if _, err := io.ReadFull(proxyConn, connResp); err != nil {
		return nil, fmt.Errorf("failed to read connection response from the server: %w", err)
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
			return nil, fmt.Errorf("failed to get FQDN length: %w", err)
		}
		bindAddr = make([]byte, respFQDNLen[0]+2)
	case constant.Socks5IPv6Address:
		bindAddr = make([]byte, 18)
	default:
		return nil, fmt.Errorf("unsupported address type: %d", respAddrType)
	}
	if _, err := io.ReadFull(proxyConn, bindAddr); err != nil {
		return nil, fmt.Errorf("failed to get bind address: %w", err)
	}
	if len(respFQDNLen) != 0 {
		connResp = append(connResp, respFQDNLen...)
	}
	connResp = append(connResp, bindAddr...)

	// Handle UDP association.
	var udpConn *net.UDPConn
	if cmd == constant.Socks5UDPAssociateCmd {
		// Create a UDP listener on a random port in IPv4 network.
		// Assume server uses 0.0.0.0:<port> as the bind address so we only need to change port number.
		var err error
		udpAddr := &net.UDPAddr{IP: net.IP{0, 0, 0, 0}, Port: 0}
		udpConn, err = net.ListenUDP("udp4", udpAddr)
		if err != nil {
			return nil, fmt.Errorf("net.ListenUDP() failed: %w", err)
		}
		// Get the port number and rewrite the response.
		_, udpPortStr, err := net.SplitHostPort(udpConn.LocalAddr().String())
		if err != nil {
			udpConn.Close()
			return nil, fmt.Errorf("net.SplitHostPort() failed: %w", err)
		}
		udpPort, err := strconv.Atoi(udpPortStr)
		if err != nil {
			udpConn.Close()
			return nil, fmt.Errorf("strconv.Atoi() failed: %w", err)
		}
		lenResp := len(connResp)
		connResp[lenResp-2] = byte(udpPort >> 8)
		connResp[lenResp-1] = byte(udpPort)
	}

	if _, err := conn.Write(connResp); err != nil {
		return nil, fmt.Errorf("failed to write connection response to the socks5 client: %w", err)
	}
	return udpConn, nil
}

// sendReply is used to send a reply message.
func sendReply(w io.Writer, resp uint8, addr *model.AddrSpec) error {
	if addr == nil {
		// Assume it is an unspecified IPv4 address.
		addr = &model.AddrSpec{
			IP: net.IPv4(0, 0, 0, 0),
		}
	}

	var buf bytes.Buffer
	buf.WriteByte(constant.Socks5Version)
	buf.WriteByte(resp)
	buf.WriteByte(0) // reserved byte
	if err := addr.WriteToSocks5(&buf); err != nil {
		return err
	}

	// Send the message.
	_, err := w.Write(buf.Bytes())
	return err
}
