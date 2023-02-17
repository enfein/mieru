package socks5

import (
	"context"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"strconv"
	"sync"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/stderror"
)

const (
	socks5Version = uint8(5)
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
	// AuthMethods can be provided to implement custom authentication
	// By default, "auth-less" mode is enabled.
	// For password-based auth use UserPassAuthenticator.
	AuthMethods []Authenticator

	// If provided, username/password authentication is enabled,
	// by appending a UserPassAuthenticator to AuthMethods. If not provided,
	// and AUthMethods is nil, then "auth-less" mode is enabled.
	Credentials CredentialStore

	// Resolver can be provided to do custom name resolution.
	// Defaults to DNSResolver if not provided.
	Resolver NameResolver

	// BindIP is used for bind or udp associate
	BindIP net.IP

	// Allow using socks5 to access resources served in localhost.
	AllowLocalDestination bool

	// Use mieru proxy to carry socks5 traffic.
	UseProxy bool

	// Mieru proxy configuration.
	ProxyConf []ProxyConfig
}

// Server is responsible for accepting connections and handling
// the details of the SOCKS5 protocol
type Server struct {
	config      *Config
	authMethods map[uint8]Authenticator
	listener    net.Listener
	chAccept    chan net.Conn
	chAcceptErr chan error
	die         chan struct{}
}

// New creates a new Server and potentially returns an error.
func New(conf *Config) (*Server, error) {
	// Ensure we have at least one authentication method enabled.
	if len(conf.AuthMethods) == 0 {
		if conf.Credentials != nil {
			conf.AuthMethods = []Authenticator{&UserPassAuthenticator{conf.Credentials}}
		} else {
			conf.AuthMethods = []Authenticator{&NoAuthAuthenticator{}}
		}
	}

	// Ensure we have a DNS resolver.
	if conf.Resolver == nil {
		conf.Resolver = DNSResolver{}
	}

	// Provide a default bind IP.
	if conf.BindIP == nil {
		conf.BindIP = net.ParseIP(netutil.AllIPAddr())
		if conf.BindIP == nil {
			return nil, fmt.Errorf("set socks5 bind IP failed")
		}
	}

	server := &Server{
		config:      conf,
		chAccept:    make(chan net.Conn, 256),
		chAcceptErr: make(chan error, 1), // non-blocking
		die:         make(chan struct{}),
	}

	server.authMethods = make(map[uint8]Authenticator)
	for _, a := range conf.AuthMethods {
		server.authMethods[a.GetCode()] = a
	}

	return server, nil
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

// ServeConn is used to serve a single connection.
func (s *Server) ServeConn(conn net.Conn) error {
	conn = netutil.WrapHierarchyConn(conn)
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
	// Proxy is enabled, forward all the traffic to proxy.
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

	if err := s.proxySocks5AuthReq(conn, proxyConn); err != nil {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		return err
	}
	udpAssociateConn, err := s.proxySocks5ConnReq(conn, proxyConn)
	if err != nil {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		return err
	}

	if udpAssociateConn != nil {
		log.Debugf("UDP association is listening on %v", udpAssociateConn.LocalAddr())
		conn.(netutil.HierarchyConn).AddSubConnection(udpAssociateConn)
		go func() {
			netutil.WaitForClose(conn)
			conn.Close()
		}()
		return BidiCopyUDP(udpAssociateConn, WrapUDPAssociateTunnel(proxyConn))
	}
	return netutil.BidiCopy(conn, proxyConn, true)
}

func (s *Server) serverServeConn(conn net.Conn) error {
	// Read the version byte and ensure we are compatible.
	version := []byte{0}
	if _, err := io.ReadFull(conn, version); err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("failed to get version byte: %w", err)
	}
	if version[0] != socks5Version {
		HandshakeErrors.Add(1)
		return fmt.Errorf("unsupported SOCKS version: %v", version)
	}

	// Authenticate the connection.
	authContext, err := s.authenticate(conn)
	if err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	request, err := NewRequest(conn)
	if err != nil {
		HandshakeErrors.Add(1)
		if err == unrecognizedAddrType {
			if err := sendReply(conn, addrTypeNotSupported, nil); err != nil {
				return fmt.Errorf("failed to send reply: %w", err)
			}
		}
		return fmt.Errorf("failed to read destination address: %w", err)
	}
	request.AuthContext = authContext
	if client, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		request.RemoteAddr = &AddrSpec{IP: client.IP, Port: client.Port}
	}

	// Process the client request.
	if err := s.handleRequest(request, conn); err != nil {
		return fmt.Errorf("handleRequest() failed: %w", err)
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
