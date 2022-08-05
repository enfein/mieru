package socks5

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
)

const (
	ConnectCommand   = uint8(1)
	BindCommand      = uint8(2)
	AssociateCommand = uint8(3)
	ipv4Address      = uint8(1)
	fqdnAddress      = uint8(3)
	ipv6Address      = uint8(4)
)

const (
	successReply uint8 = iota
	serverFailure
	ruleFailure
	networkUnreachable
	hostUnreachable
	connectionRefused
	ttlExpired
	commandNotSupported
	addrTypeNotSupported
)

var (
	unrecognizedAddrType = fmt.Errorf("unrecognized address type")
)

// AddressRewriter is used to rewrite a destination transparently.
type AddressRewriter interface {
	Rewrite(ctx context.Context, request *Request) (context.Context, *AddrSpec)
}

// AddrSpec is used to return the target AddrSpec
// which may be specified as IPv4, IPv6, or a FQDN.
type AddrSpec struct {
	FQDN string
	IP   net.IP
	Port int
}

func (a *AddrSpec) String() string {
	if a.FQDN != "" {
		return fmt.Sprintf("%s (%s):%d", a.FQDN, a.IP, a.Port)
	}
	return fmt.Sprintf("%s:%d", a.IP, a.Port)
}

// Address returns a string suitable to dial; prefer returning IP-based
// address, fallback to FQDN
func (a AddrSpec) Address() string {
	if 0 != len(a.IP) {
		return net.JoinHostPort(a.IP.String(), strconv.Itoa(a.Port))
	}
	return net.JoinHostPort(a.FQDN, strconv.Itoa(a.Port))
}

// A Request represents request received by a server.
type Request struct {
	// Protocol version.
	Version uint8
	// Requested command.
	Command uint8
	// AuthContext provided during negotiation.
	AuthContext *AuthContext
	// AddrSpec of the the network that sent the request.
	RemoteAddr *AddrSpec
	// AddrSpec of the desired destination.
	DestAddr *AddrSpec
	// AddrSpec of the actual destination (might be affected by rewrite).
	realDestAddr *AddrSpec
	bufConn      io.Reader
}

type conn interface {
	Write([]byte) (int, error)
	RemoteAddr() net.Addr
}

// NewRequest creates a new Request from the tcp connection.
func NewRequest(bufConn io.Reader) (*Request, error) {
	// Read the version byte.
	header := []byte{0, 0, 0}
	if _, err := io.ReadFull(bufConn, header); err != nil {
		return nil, fmt.Errorf("failed to get command version: %w", err)
	}

	// Ensure we are compatible.
	if header[0] != socks5Version {
		return nil, fmt.Errorf("unsupported command version: %v", header[0])
	}

	// Read in the destination address.
	dest, err := readAddrSpec(bufConn)
	if err != nil {
		return nil, err
	}

	request := &Request{
		Version:  socks5Version,
		Command:  header[1],
		DestAddr: dest,
		bufConn:  bufConn,
	}

	return request, nil
}

// handleRequest is used for request processing after authentication.
func (s *Server) handleRequest(req *Request, conn conn) error {
	ctx := context.Background()

	// Resolve the address if we have a FQDN.
	dest := req.DestAddr
	if dest.FQDN != "" {
		ctx_, addr, err := s.config.Resolver.Resolve(ctx, dest.FQDN)
		if err != nil {
			atomic.AddUint64(&metrics.Socks5DNSResolveErrors, 1)
			if err := sendReply(conn, hostUnreachable, nil); err != nil {
				return fmt.Errorf("failed to send reply: %w", err)
			}
			return fmt.Errorf("failed to resolve destination %q: %w", dest.FQDN, err)
		}
		ctx = ctx_
		dest.IP = addr
	}

	// Apply any address rewrites.
	req.realDestAddr = req.DestAddr
	if s.config.Rewriter != nil {
		ctx, req.realDestAddr = s.config.Rewriter.Rewrite(ctx, req)
	}

	// Return error if access local destination is not allowed.
	if !s.config.AllowLocalDestination && isLocalhostDest(req) {
		return fmt.Errorf("access to localhost resource via proxy is not allowed")
	}

	// Switch on the command.
	switch req.Command {
	case ConnectCommand:
		return s.handleConnect(ctx, conn, req)
	case BindCommand:
		return s.handleBind(ctx, conn, req)
	case AssociateCommand:
		return s.handleAssociate(ctx, conn, req)
	default:
		atomic.AddUint64(&metrics.Socks5UnsupportedCommandErrors, 1)
		if err := sendReply(conn, commandNotSupported, nil); err != nil {
			return fmt.Errorf("failed to send reply: %w", err)
		}
		return fmt.Errorf("unsupported command: %v", req.Command)
	}
}

// handleConnect is used to handle a connect command.
func (s *Server) handleConnect(ctx context.Context, conn conn, req *Request) error {
	// Check if this is allowed.
	if ctx_, ok := s.config.Rules.Allow(ctx, req); !ok {
		if err := sendReply(conn, ruleFailure, nil); err != nil {
			return fmt.Errorf("failed to send reply: %w", err)
		}
		return fmt.Errorf("connect to %v blocked by rules", req.DestAddr)
	} else {
		ctx = ctx_
	}

	// Attempt to connect.
	if s.config.NetworkType == "" {
		s.config.NetworkType = "tcp"
	}
	var d net.Dialer
	target, err := d.DialContext(ctx, s.config.NetworkType, req.realDestAddr.Address())
	if err != nil {
		msg := err.Error()
		resp := hostUnreachable
		atomic.AddUint64(&metrics.Socks5HostUnreachableErrors, 1)
		if strings.Contains(msg, "refused") {
			resp = connectionRefused
			atomic.AddUint64(&metrics.Socks5ConnectionRefusedErrors, 1)
		} else if strings.Contains(msg, "network is unreachable") {
			resp = networkUnreachable
			atomic.AddUint64(&metrics.Socks5NetworkUnreachableErrors, 1)
		}
		if err := sendReply(conn, resp, nil); err != nil {
			return fmt.Errorf("failed to send reply: %w", err)
		}
		return fmt.Errorf("connect to %v failed: %w", req.DestAddr, err)
	}
	defer target.Close()

	// Send success.
	local := target.LocalAddr().(*net.TCPAddr)
	bind := AddrSpec{IP: local.IP, Port: local.Port}
	if err := sendReply(conn, successReply, &bind); err != nil {
		return fmt.Errorf("failed to send reply: %w", err)
	}

	// Start proxying.
	errCh := make(chan error, 2)
	go proxy(target, req.bufConn, errCh)
	go proxy(conn, target, errCh)

	// Wait for connection to close.
	for i := 0; i < 2; i++ {
		e := <-errCh
		if e != nil {
			// Return from this function closes target (and conn).
			return e
		}
	}
	return nil
}

// handleBind is used to handle a bind command.
func (s *Server) handleBind(ctx context.Context, conn conn, req *Request) error {
	if log.IsLevelEnabled(log.DebugLevel) {
		log.Debugf("received unsupported socks5 bind request from %v", conn.RemoteAddr())
	}

	// Check if this is allowed.
	if ctx_, ok := s.config.Rules.Allow(ctx, req); !ok {
		if err := sendReply(conn, ruleFailure, nil); err != nil {
			return fmt.Errorf("failed to send reply: %w", err)
		}
		return fmt.Errorf("bind to %v blocked by rules", req.DestAddr)
	} else {
		ctx = ctx_
	}

	atomic.AddUint64(&metrics.Socks5UnsupportedCommandErrors, 1)
	if err := sendReply(conn, commandNotSupported, nil); err != nil {
		return fmt.Errorf("failed to send reply: %w", err)
	}
	return nil
}

// handleAssociate is used to handle a associate command.
func (s *Server) handleAssociate(ctx context.Context, conn conn, req *Request) error {
	if log.IsLevelEnabled(log.DebugLevel) {
		log.Debugf("received unsupported socks5 associate request from %v", conn.RemoteAddr())
	}

	// Check if this is allowed.
	if ctx_, ok := s.config.Rules.Allow(ctx, req); !ok {
		if err := sendReply(conn, ruleFailure, nil); err != nil {
			return fmt.Errorf("failed to send reply: %w", err)
		}
		return fmt.Errorf("associate to %v blocked by rules", req.DestAddr)
	} else {
		ctx = ctx_
	}

	atomic.AddUint64(&metrics.Socks5UnsupportedCommandErrors, 1)
	if err := sendReply(conn, commandNotSupported, nil); err != nil {
		return fmt.Errorf("failed to send reply: %w", err)
	}
	return nil
}

// readAddrSpec is used to read AddrSpec.
// Expects an address type byte, follwed by the address and port.
func readAddrSpec(r io.Reader) (*AddrSpec, error) {
	d := &AddrSpec{}

	// Get the address type.
	addrType := []byte{0}
	if _, err := io.ReadFull(r, addrType); err != nil {
		return nil, err
	}

	// Handle on a per type basis.
	switch addrType[0] {
	case ipv4Address:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(r, addr); err != nil {
			return nil, err
		}
		d.IP = net.IP(addr)

	case ipv6Address:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(r, addr); err != nil {
			return nil, err
		}
		d.IP = net.IP(addr)

	case fqdnAddress:
		if _, err := io.ReadFull(r, addrType); err != nil {
			return nil, err
		}
		addrLen := int(addrType[0])
		fqdn := make([]byte, addrLen)
		if _, err := io.ReadFull(r, fqdn); err != nil {
			return nil, err
		}
		d.FQDN = string(fqdn)

	default:
		return nil, unrecognizedAddrType
	}

	// Read the port number.
	port := []byte{0, 0}
	if _, err := io.ReadFull(r, port); err != nil {
		return nil, err
	}
	d.Port = (int(port[0]) << 8) | int(port[1])

	return d, nil
}

// sendReply is used to send a reply message.
func sendReply(w io.Writer, resp uint8, addr *AddrSpec) error {
	// Format the address.
	var addrType uint8
	var addrBody []byte
	var addrPort uint16
	switch {
	case addr == nil:
		addrType = ipv4Address
		addrBody = []byte{0, 0, 0, 0}
		addrPort = 0

	case addr.FQDN != "":
		addrType = fqdnAddress
		addrBody = append([]byte{byte(len(addr.FQDN))}, addr.FQDN...)
		addrPort = uint16(addr.Port)

	case addr.IP.To4() != nil:
		addrType = ipv4Address
		addrBody = []byte(addr.IP.To4())
		addrPort = uint16(addr.Port)

	case addr.IP.To16() != nil:
		addrType = ipv6Address
		addrBody = []byte(addr.IP.To16())
		addrPort = uint16(addr.Port)

	default:
		return fmt.Errorf("failed to format address: %v", addr)
	}

	// Format the message.
	msg := make([]byte, 6+len(addrBody))
	msg[0] = socks5Version
	msg[1] = resp
	msg[2] = 0 // reserved byte
	msg[3] = addrType
	copy(msg[4:], addrBody)
	msg[4+len(addrBody)] = byte(addrPort >> 8)
	msg[4+len(addrBody)+1] = byte(addrPort & 0xff)

	// Send the message.
	_, err := w.Write(msg)
	return err
}

type closeWriter interface {
	CloseWrite() error
}

// proxy is used to suffle data from src to destination, and sends errors
// down a dedicated channel.
func proxy(dst io.Writer, src io.Reader, errCh chan error) {
	_, err := io.Copy(dst, src)
	if tcpConn, ok := dst.(closeWriter); ok {
		tcpConn.CloseWrite()
	}
	errCh <- err
}

func isLocalhostDest(req *Request) bool {
	if req == nil || req.DestAddr == nil {
		return false
	}
	if req.DestAddr.FQDN == "localhost" || req.DestAddr.IP.IsLoopback() {
		return true
	}
	return false
}
