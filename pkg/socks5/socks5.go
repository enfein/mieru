package socks5

import (
	"context"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"strconv"
	"sync"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/protocolv2"
	"github.com/enfein/mieru/pkg/stderror"
	"github.com/enfein/mieru/pkg/util"
)

const (
	// socks5 version number.
	socks5Version byte = 5

	// No authentication required.
	noAuth byte = 0
)

var (
	HandshakeErrors          = metrics.RegisterMetric("socks5", "HandshakeErrors")
	DNSResolveErrors         = metrics.RegisterMetric("socks5", "DNSResolveErrors")
	UnsupportedCommandErrors = metrics.RegisterMetric("socks5", "UnsupportedCommandErrors")
	NetworkUnreachableErrors = metrics.RegisterMetric("socks5", "NetworkUnreachableErrors")
	HostUnreachableErrors    = metrics.RegisterMetric("socks5", "HostUnreachableErrors")
	ConnectionRefusedErrors  = metrics.RegisterMetric("socks5", "ConnectionRefusedErrors")
	UDPAssociateErrors       = metrics.RegisterMetric("socks5", "UDPAssociateErrors")

	// Incoming UDP association bytes.
	UDPAssociateInBytes = metrics.RegisterMetric("socks5 UDP associate", "InBytes")

	// Outgoing UDP association bytes.
	UDPAssociateOutBytes = metrics.RegisterMetric("socks5 UDP associate", "OutBytes")

	// Incoming UDP association packets.
	UDPAssociateInPkts = metrics.RegisterMetric("socks5 UDP associate", "InPkts")

	// Outgoing UDP association packets.
	UDPAssociateOutPkts = metrics.RegisterMetric("socks5 UDP associate", "OutPkts")
)

// ProxyConfig is used to configure mieru proxy options.
type ProxyConfig struct {
	// NetworkType ("tcp", "udp", etc.) used when dial to the proxy.
	NetworkType string

	// Address is proxy server listening address, in host:port format.
	Address string

	// Password is used to derive the cipher block used for encryption.
	Password []byte

	// Dial is the function to dial to the proxy server.
	Dial func(ctx context.Context, proxyNetwork, localAddr, proxyAddr string, block cipher.BlockCipher) (net.Conn, error)
}

// Config is used to setup and configure a socks5 server.
type Config struct {
	// Resolver can be provided to do custom name resolution.
	Resolver *util.DNSResolver

	// BindIP is used for bind or udp associate
	BindIP net.IP

	// Use mieru proxy to carry socks5 traffic.
	UseProxy bool

	// Allow using socks5 to access resources served in localhost.
	AllowLocalDestination bool

	// Do socks5 authentication at proxy client side.
	ClientSideAuthentication bool

	// Mieru proxy configuration.
	ProxyConf []ProxyConfig

	// Mieru proxy multiplexer.
	ProxyMux *protocolv2.Mux
}

// Server is responsible for accepting connections and handling
// the details of the SOCKS5 protocol
type Server struct {
	config      *Config
	listener    net.Listener
	chAccept    chan net.Conn
	chAcceptErr chan error
	die         chan struct{}
}

var (
	_ util.ConnHandler = &Server{}
)

// New creates a new Server and potentially returns an error.
func New(conf *Config) (*Server, error) {
	// Ensure we have a DNS resolver.
	if conf.Resolver == nil {
		conf.Resolver = &util.DNSResolver{}
	}

	// Provide a default bind IP.
	if conf.BindIP == nil {
		conf.BindIP = net.ParseIP(util.AllIPAddr())
		if conf.BindIP == nil {
			return nil, fmt.Errorf("set socks5 bind IP failed")
		}
	}

	return &Server{
		config:      conf,
		chAccept:    make(chan net.Conn, 256),
		chAcceptErr: make(chan error, 1), // non-blocking
		die:         make(chan struct{}),
	}, nil
}

// ListenAndServe is used to create a listener and serve on it.
func (s *Server) ListenAndServe(network, addr string) error {
	l, err := net.Listen(network, addr)
	if err != nil {
		return err
	}
	return s.Serve(l)
}

// Serve is used to serve connections from a listener.
func (s *Server) Serve(l net.Listener) error {
	s.listener = l
	go s.acceptLoop()
	for {
		select {
		case conn := <-s.chAccept:
			go func() {
				err := s.ServeConn(conn)
				if err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
					log.Debugf("socks5 server listener %v ServeConn() failed: %v", s.listener.Addr(), err)
				}
			}()
		case err := <-s.chAcceptErr:
			log.Errorf("encountered error when socks5 server accept new connection: %v", err)
			log.Infof("closing socks5 server listener")
			if err := s.listener.Close(); err != nil {
				log.Warnf("socks5 server listener %v Close() failed: %v", s.listener.Addr(), err)
			}
			return err // the err from chAcceptErr
		case <-s.die:
			log.Infof("closing socks5 server listener")
			if err := s.listener.Close(); err != nil {
				log.Warnf("socks5 server listener %v Close() failed: %v", s.listener.Addr(), err)
			}
			return nil
		}
	}
}

// Take implements util.ConnHandler interface.
func (s *Server) Take(conn net.Conn) (closed bool, err error) {
	err = s.ServeConn(conn)
	return true, err
}

// ServeConn is used to serve a single connection.
func (s *Server) ServeConn(conn net.Conn) error {
	conn = util.WrapHierarchyConn(conn)
	defer conn.Close()
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("socks5 server starts to serve connection [%v - %v]", conn.LocalAddr(), conn.RemoteAddr())
	}

	if s.config.UseProxy {
		return s.clientServeConn(conn)
	} else {
		return s.serverServeConn(conn)
	}
}

// Close closes the network listener used by the server.
func (s *Server) Close() error {
	close(s.die)
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.chAcceptErr <- err
			return
		}
		s.chAccept <- conn
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("socks5 server accepted connection [%v - %v]", conn.LocalAddr(), conn.RemoteAddr())
		}
	}
}

func (s *Server) clientServeConn(conn net.Conn) error {
	if s.config.ClientSideAuthentication {
		if err := s.handleAuthentication(conn); err != nil {
			return err
		}
	}

	// Forward remaining bytes to proxy.
	// If there are multiple proxy endpoints, randomly select one of them.
	n := len(s.config.ProxyConf)
	if n == 0 {
		log.Fatalf("No proxy configuration is available in socks5 server config")
	}
	i := mrand.Intn(n)
	proxyConf := s.config.ProxyConf[i]
	if proxyConf.NetworkType == "" {
		log.Fatalf("Proxy network type is not set in socks5 server config")
	}
	if proxyConf.Address == "" {
		log.Fatalf("Proxy address is not set in socks5 server config")
	}
	if len(proxyConf.Password) == 0 {
		log.Fatalf("Proxy password is not set in socks5 server config")
	}
	if proxyConf.Dial == nil {
		log.Fatalf("Proxy dial function is not set in socks5 server config")
	}

	ctx := context.Background()
	var block cipher.BlockCipher
	var err error
	if proxyConf.NetworkType == "tcp" {
		block, err = cipher.BlockCipherFromPassword(proxyConf.Password, false)
	} else if proxyConf.NetworkType == "udp" {
		block, err = cipher.BlockCipherFromPassword(proxyConf.Password, true)
	} else {
		return fmt.Errorf("proxy network type %q is not supported", proxyConf.NetworkType)
	}
	if err != nil {
		return fmt.Errorf("cipher.BlockCipherFromPassword() failed: %w", err)
	}
	proxyConn, err := proxyConf.Dial(ctx, proxyConf.NetworkType, "", proxyConf.Address, block)
	if err != nil {
		return fmt.Errorf("proxy Dial(%q, %q) failed: %w", proxyConf.NetworkType, proxyConf.Address, err)
	}

	if !s.config.ClientSideAuthentication {
		if err := s.proxySocks5AuthReq(conn, proxyConn); err != nil {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			return err
		}
	}
	udpAssociateConn, err := s.proxySocks5ConnReq(conn, proxyConn)
	if err != nil {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		return err
	}

	if udpAssociateConn != nil {
		log.Debugf("UDP association is listening on %v", udpAssociateConn.LocalAddr())
		conn.(util.HierarchyConn).AddSubConnection(udpAssociateConn)
		go func() {
			util.WaitForClose(conn)
			conn.Close()
		}()
		return BidiCopyUDP(udpAssociateConn, WrapUDPAssociateTunnel(proxyConn))
	}
	return util.BidiCopy(conn, proxyConn, true)
}

func (s *Server) serverServeConn(conn net.Conn) error {
	if !s.config.ClientSideAuthentication {
		if err := s.handleAuthentication(conn); err != nil {
			return err
		}
	}

	request, err := NewRequest(conn)
	if err != nil {
		HandshakeErrors.Add(1)
		if errors.Is(err, errUnrecognizedAddrType) {
			if err := sendReply(conn, addrTypeNotSupported, nil); err != nil {
				return fmt.Errorf("failed to send reply: %w", err)
			}
		}
		return fmt.Errorf("failed to read destination address: %w", err)
	}

	// Process the client request.
	if err := s.handleRequest(context.Background(), request, conn); err != nil {
		return fmt.Errorf("handleRequest() failed: %w", err)
	}
	return nil
}

func (s *Server) handleAuthentication(conn net.Conn) error {
	// Read the version byte and ensure we are compatible.
	version := []byte{0}
	if _, err := io.ReadFull(conn, version); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("get socks version failed: %w", err)
	}
	if version[0] != socks5Version {
		HandshakeErrors.Add(1)
		return fmt.Errorf("unsupported socks version: %v", version)
	}

	// Authenticate the connection.
	nAuthMethods := []byte{0}
	if _, err := io.ReadFull(conn, nAuthMethods); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("get number of authentication method failed: %w", err)
	}
	if nAuthMethods[0] == 0 {
		HandshakeErrors.Add(1)
		return fmt.Errorf("number of authentication method is 0")
	}
	allowNoAuth := false
	authMethods := make([]byte, nAuthMethods[0])
	if _, err := io.ReadFull(conn, authMethods); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("get authentication method failed: %w", err)
	}
	for _, method := range authMethods {
		if method == noAuth {
			allowNoAuth = true
			break
		}
	}
	if !allowNoAuth {
		HandshakeErrors.Add(1)
		return fmt.Errorf("no authentication is not supported by socks5 client")
	}
	if _, err := conn.Write([]byte{socks5Version, noAuth}); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("write authentication response failed: %w", err)
	}
	return nil
}

// ServerGroup is a collection of socks5 servers that share the same lifecycle.
type ServerGroup struct {
	servers map[string]*Server
	mu      sync.Mutex
}

// NewGroup creates a new ServerGroup.
func NewGroup() *ServerGroup {
	return &ServerGroup{
		servers: make(map[string]*Server),
	}
}

// Add adds a socks5 server into the ServerGroup.
func (g *ServerGroup) Add(underlayProtocol string, port int, s *Server) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := underlayProtocol + "-" + strconv.Itoa(port)
	if _, found := g.servers[key]; found {
		return stderror.ErrAlreadyExist
	}
	if s == nil {
		return stderror.ErrInvalidArgument
	}
	g.servers[key] = s
	return nil
}

// CloseAndRemoveAll closes all the socks5 servers and clear the group.
func (g *ServerGroup) CloseAndRemoveAll() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	var lastErr error
	for _, l := range g.servers {
		err := l.Close()
		if err != nil {
			lastErr = err
		}
	}
	g.servers = make(map[string]*Server)
	return lastErr
}

// IsEmpty returns true if the group has no socks5 server.
func (g *ServerGroup) IsEmpty() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.servers) == 0
}
