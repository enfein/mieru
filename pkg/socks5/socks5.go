package socks5

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sync"

	"context"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/log"
)

const (
	socks5Version = uint8(5)
)

// Config is used to setup and configure a Server
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

	// Rules is provided to enable custom logic around permitting
	// various commands. If not provided, PermitAll is used.
	Rules RuleSet

	// Rewriter can be used to transparently rewrite addresses.
	// This is invoked before the RuleSet is invoked.
	// Defaults to NoRewrite.
	Rewriter AddressRewriter

	// BindIP is used for bind or udp associate
	BindIP net.IP

	// Network type when dial to the destination.
	// If not specified, "tcp" is used.
	NetworkType string

	// An optional function for dialing out to the destination.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)

	// Allow using socks5 to access resources served in localhost.
	AllowLocalDestination bool

	UseProxy bool

	// Network type when dial to the proxy.
	// If not specified, "tcp" is used.
	ProxyNetworkType string

	// Proxy address.
	ProxyAddress string

	// ProxyPassword is used to derive the cipher block used for encryption.
	ProxyPassword []byte

	// A function to dial to a proxy to forward all the requests.
	// Must be set if proxy is used.
	ProxyDial func(ctx context.Context, proxyNetwork, localAddr, proxyAddr string, block cipher.BlockCipher) (net.Conn, error)
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

	// Ensure we have a rule set
	if conf.Rules == nil {
		conf.Rules = PermitAll()
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
			go s.ServeConn(conn)
		case err := <-s.chAcceptErr:
			log.Infof("encountered error when socks5 server accepts new connection: %v", err)
			log.Infof("closing socks5 server listener")
			if err = s.listener.Close(); err != nil {
				if log.IsLevelEnabled(log.DebugLevel) {
					log.Debugf("listener %v Close() failed: %v", s.listener.Addr(), err)
				}
			}
			return err
		case <-s.die:
			log.Infof("closing socks5 server listener")
			if err := s.listener.Close(); err != nil {
				if log.IsLevelEnabled(log.DebugLevel) {
					log.Debugf("listener %v Close() failed: %v", s.listener.Addr(), err)
				}
			}
			return nil
		}
	}
}

// ServeConn is used to serve a single connection.
func (s *Server) ServeConn(conn net.Conn) error {
	defer conn.Close()

	if s.config.UseProxy {
		// When proxy is enabled, blindly forward all the traffic to proxy.
		if s.config.ProxyAddress == "" {
			log.Fatalf("ProxyAddress is not set in socks server config")
		}
		if s.config.ProxyDial == nil {
			log.Fatalf("ProxyDial is not set in socks server config")
		}
		if s.config.ProxyNetworkType == "" {
			s.config.ProxyNetworkType = "tcp"
		}
		if s.config.ProxyPassword == nil {
			s.config.ProxyPassword = make([]byte, 0)
		}
		ctx := context.Background()
		block, err := cipher.BlockCipherFromPassword(s.config.ProxyPassword)
		if err != nil {
			return fmt.Errorf("cipher.BlockCipherFromPassword() failed: %w", err)
		}
		proxyConn, err := s.config.ProxyDial(ctx, s.config.ProxyNetworkType, "", s.config.ProxyAddress, block)
		if err != nil {
			return fmt.Errorf("ProxyDial(%q, %q) failed: %w", s.config.ProxyNetworkType, s.config.ProxyAddress, err)
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
	} else {
		bufConn := bufio.NewReader(conn)

		// Read the version byte.
		version := []byte{0}
		if _, err := bufConn.Read(version); err != nil {
			if log.IsLevelEnabled(log.DebugLevel) {
				log.Debugf("socks failed to get version byte: %v", err)
			}
			return err
		}

		// Ensure we are compatible.
		if version[0] != socks5Version {
			err := fmt.Errorf("unsupported SOCKS version: %v", version)
			if log.IsLevelEnabled(log.DebugLevel) {
				log.Debugf("socks %v", err)
			}
			return err
		}

		// Authenticate the connection.
		authContext, err := s.authenticate(conn, bufConn)
		if err != nil {
			err = fmt.Errorf("failed to authenticate: %w", err)
			if log.IsLevelEnabled(log.DebugLevel) {
				log.Debugf("socks %v", err)
			}
			return err
		}

		request, err := NewRequest(bufConn)
		if err != nil {
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
			err = fmt.Errorf("failed to handle request: %w", err)
			if log.IsLevelEnabled(log.DebugLevel) {
				log.Debugf("socks %v", err)
			}
			return err
		}
	}

	return nil
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
	}
}
