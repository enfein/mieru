package socks5

import (
	"bufio"
	"context"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/stderror"
)

const (
	socks5Version = uint8(5)
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

	// Network type when dial to the destination.
	// If not specified, "tcp" is used.
	NetworkType string

	// Allow using socks5 to access resources served in localhost.
	AllowLocalDestination bool

	// Use mieru proxy to carry socks5 traffic.
	UseProxy bool

	// Mieru proxy configuration.
	ProxyConf []ProxyConfig
}

// Server is reponsible for accepting connections and handling
// the details of the SOCKS5 protocol
type Server struct {
	config      *Config
	authMethods map[uint8]Authenticator
	listener    net.Listener
	chAccept    chan net.Conn
	chAcceptErr chan error
	die         chan struct{}
}

// New creates a new Server and potentially returns an error
func New(conf *Config) (*Server, error) {
	// Ensure we have at least one authentication method enabled
	if len(conf.AuthMethods) == 0 {
		if conf.Credentials != nil {
			conf.AuthMethods = []Authenticator{&UserPassAuthenticator{conf.Credentials}}
		} else {
			conf.AuthMethods = []Authenticator{&NoAuthAuthenticator{}}
		}
	}

	// Ensure we have a DNS resolver
	if conf.Resolver == nil {
		conf.Resolver = DNSResolver{}
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
				if err != nil {
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
	// When proxy is enabled, forward all the traffic to proxy.
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

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		io.Copy(proxyConn, conn)
		if err := proxyConn.Close(); err != nil {
			if log.IsLevelEnabled(log.DebugLevel) {
				log.Debugf("[%v - %v] Close() failed: %v", proxyConn.LocalAddr(), proxyConn.RemoteAddr(), err)
			}
		}
		wg.Done()
	}()
	go func() {
		io.Copy(conn, proxyConn)
		if err := conn.Close(); err != nil {
			if log.IsLevelEnabled(log.DebugLevel) {
				log.Debugf("[%v - %v] Close() failed: %v", conn.LocalAddr(), conn.RemoteAddr(), err)
			}
		}
		wg.Done()
	}()
	wg.Wait()
	return nil
}

func (s *Server) serverServeConn(conn net.Conn) error {
	bufConn := bufio.NewReader(conn)

	// Read the version byte.
	version := []byte{0}
	if _, err := io.ReadFull(bufConn, version); err != nil {
		atomic.AddUint64(&metrics.Socks5HandshakeErrors, 1)
		return fmt.Errorf("failed to get version byte: %w", err)
	}

	// Ensure we are compatible.
	if version[0] != socks5Version {
		atomic.AddUint64(&metrics.Socks5HandshakeErrors, 1)
		return fmt.Errorf("unsupported SOCKS version: %v", version)
	}

	// Authenticate the connection.
	authContext, err := s.authenticate(conn, bufConn)
	if err != nil {
		atomic.AddUint64(&metrics.Socks5HandshakeErrors, 1)
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	request, err := NewRequest(bufConn)
	if err != nil {
		atomic.AddUint64(&metrics.Socks5HandshakeErrors, 1)
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
		return fmt.Errorf("failed to handle request: %w", err)
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
