package socks5

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/egress"
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
	HandshakeErrors          = metrics.RegisterMetric("socks5", "HandshakeErrors", metrics.COUNTER)
	DNSResolveErrors         = metrics.RegisterMetric("socks5", "DNSResolveErrors", metrics.COUNTER)
	UnsupportedCommandErrors = metrics.RegisterMetric("socks5", "UnsupportedCommandErrors", metrics.COUNTER)
	NetworkUnreachableErrors = metrics.RegisterMetric("socks5", "NetworkUnreachableErrors", metrics.COUNTER)
	HostUnreachableErrors    = metrics.RegisterMetric("socks5", "HostUnreachableErrors", metrics.COUNTER)
	ConnectionRefusedErrors  = metrics.RegisterMetric("socks5", "ConnectionRefusedErrors", metrics.COUNTER)
	UDPAssociateErrors       = metrics.RegisterMetric("socks5", "UDPAssociateErrors", metrics.COUNTER)

	UDPAssociateInBytes  = metrics.RegisterMetric("socks5 UDP associate", "InBytes", metrics.COUNTER)
	UDPAssociateOutBytes = metrics.RegisterMetric("socks5 UDP associate", "OutBytes", metrics.COUNTER)
	UDPAssociateInPkts   = metrics.RegisterMetric("socks5 UDP associate", "InPkts", metrics.COUNTER)
	UDPAssociateOutPkts  = metrics.RegisterMetric("socks5 UDP associate", "OutPkts", metrics.COUNTER)
)

// Config is used to setup and configure a socks5 server.
type Config struct {
	// Mieru proxy multiplexer.
	ProxyMux *protocolv2.Mux

	// Egress controller.
	EgressController egress.Controller

	// Resolver can be provided to do custom name resolution.
	Resolver *util.DNSResolver

	// BindIP is used for bind or udp associate
	BindIP net.IP

	// Handshake timeout to establish socks5 connection.
	// Use 0 or negative value to disable the timeout.
	HandshakeTimeout time.Duration

	// Use mieru proxy to carry socks5 traffic.
	UseProxy bool

	// Allow using socks5 to access resources served in localhost.
	AllowLocalDestination bool

	// Do socks5 authentication at proxy client side.
	ClientSideAuthentication bool
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

// New creates a new Server and potentially returns an error.
func New(conf *Config) (*Server, error) {
	if conf.UseProxy && conf.ProxyMux == nil {
		return nil, fmt.Errorf("ProxyMux must be set when proxy is enabled")
	}

	// Ensure we have a egress controller.
	if conf.EgressController == nil {
		conf.EgressController = egress.AlwaysDirectController{}
	}

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
	ctx := context.Background()
	var proxyConn net.Conn
	var err error
	proxyConn, err = s.config.ProxyMux.DialContext(ctx)
	if err != nil {
		return fmt.Errorf("mux DialContext() failed: %w", err)
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
	return util.BidiCopy(conn, proxyConn)
}

func (s *Server) serverServeConn(conn net.Conn) error {
	if !s.config.ClientSideAuthentication {
		if err := s.handleAuthentication(conn); err != nil {
			return err
		}
	}

	request, err := s.newRequest(conn)
	if err != nil {
		HandshakeErrors.Add(1)
		if errors.Is(err, errUnrecognizedAddrType) {
			if err := sendReply(conn, addrTypeNotSupported, nil); err != nil {
				return fmt.Errorf("failed to send reply: %w", err)
			}
		}
		return fmt.Errorf("failed to read destination address: %w", err)
	}

	action := s.config.EgressController.FindAction(egress.Input{
		Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
		Data:     request.Raw,
	})
	log.Debugf("Egress decision of socks5 request %v is %s", request.Raw, action.Action.String())
	switch action.Action {
	case appctlpb.EgressAction_DIRECT:
		if err := s.handleRequest(context.Background(), request, conn); err != nil {
			return fmt.Errorf("handleRequest() failed: %w", err)
		}
	case appctlpb.EgressAction_PROXY:
		if action.Proxy == nil {
			return fmt.Errorf("egress action is PROXY but proxy info is unavailable")
		}
		return s.handleForwarding(request, conn, action.Proxy.GetHost(), action.Proxy.GetPort())
	case appctlpb.EgressAction_REJECT:
		return fmt.Errorf("connection is rejected by egress rules")
	}
	return nil
}

func (s *Server) handleAuthentication(conn net.Conn) error {
	// Read the version byte and ensure we are compatible.
	util.SetReadTimeout(conn, s.config.HandshakeTimeout)
	defer util.SetReadTimeout(conn, 0)
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

func (s *Server) handleForwarding(req *Request, conn net.Conn, forwardHost string, forwardPort int32) error {
	proxyConn, err := net.Dial("tcp", util.MaybeDecorateIPv6(forwardHost)+":"+strconv.Itoa(int(forwardPort)))
	if err != nil {
		HandshakeErrors.Add(1)
		return fmt.Errorf("dial to egress proxy failed: %w", err)
	}

	// Authenticate with the egress proxy.
	if _, err := proxyConn.Write([]byte{socks5Version, 1, noAuth}); err != nil {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		return fmt.Errorf("failed to write socks5 auth header to egress proxy: %w", err)
	}
	util.SetReadTimeout(proxyConn, s.config.HandshakeTimeout)
	defer util.SetReadTimeout(proxyConn, 0)
	resp := []byte{0, 0}
	if _, err := io.ReadFull(proxyConn, resp); err != nil {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		return fmt.Errorf("failed to read socks5 auth response from egress proxy: %w", err)
	}
	if resp[0] != socks5Version || resp[1] != noAuth {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		return fmt.Errorf("got unexpected socks5 auth response from egress proxy: %v", resp)
	}
	if _, err := proxyConn.Write(req.Raw); err != nil {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		return fmt.Errorf("failed to write socks5 request to egress proxy: %w", err)
	}
	return util.BidiCopy(conn, proxyConn)
}
