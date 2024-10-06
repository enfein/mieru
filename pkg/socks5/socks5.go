package socks5

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/egress"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/metrics"
	"github.com/enfein/mieru/v3/pkg/protocol"
	"github.com/enfein/mieru/v3/pkg/stderror"
	"github.com/enfein/mieru/v3/pkg/util"
)

var (
	HandshakeErrors          = metrics.RegisterMetric("socks5", "HandshakeErrors", metrics.COUNTER)
	DNSResolveErrors         = metrics.RegisterMetric("socks5", "DNSResolveErrors", metrics.COUNTER)
	UnsupportedCommandErrors = metrics.RegisterMetric("socks5", "UnsupportedCommandErrors", metrics.COUNTER)
	NetworkUnreachableErrors = metrics.RegisterMetric("socks5", "NetworkUnreachableErrors", metrics.COUNTER)
	HostUnreachableErrors    = metrics.RegisterMetric("socks5", "HostUnreachableErrors", metrics.COUNTER)
	ConnectionRefusedErrors  = metrics.RegisterMetric("socks5", "ConnectionRefusedErrors", metrics.COUNTER)
	UDPAssociateErrors       = metrics.RegisterMetric("socks5", "UDPAssociateErrors", metrics.COUNTER)

	UDPAssociateUploadBytes     = metrics.RegisterMetric("socks5 UDP associate", "UploadBytes", metrics.COUNTER)
	UDPAssociateDownloadBytes   = metrics.RegisterMetric("socks5 UDP associate", "DownloadBytes", metrics.COUNTER)
	UDPAssociateUploadPackets   = metrics.RegisterMetric("socks5 UDP associate", "UploadPackets", metrics.COUNTER)
	UDPAssociateDownloadPackets = metrics.RegisterMetric("socks5 UDP associate", "DownloadPackets", metrics.COUNTER)
)

// Config is used to setup and configure a socks5 server.
type Config struct {
	// Mieru proxy multiplexer.
	ProxyMux *protocol.Mux

	// Egress controller.
	EgressController egress.Controller

	// Resolver can be provided to do custom name resolution.
	Resolver *util.DNSResolver

	// BindIP is used for bind or udp associate
	BindIP net.IP

	// Authentication options.
	AuthOpts Auth

	// Handshake timeout to establish socks5 connection.
	// Use 0 or negative value to disable the timeout.
	HandshakeTimeout time.Duration

	// Use mieru proxy to carry socks5 traffic.
	UseProxy bool

	// Allow using socks5 to access resources served in localhost.
	AllowLocalDestination bool
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
	log.Debugf("socks5 server starts to serve connection [%v - %v]", conn.LocalAddr(), conn.RemoteAddr())

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
	if s.config.AuthOpts.ClientSideAuthentication {
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

	if !s.config.AuthOpts.ClientSideAuthentication {
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
	if !s.config.AuthOpts.ClientSideAuthentication {
		if err := s.handleAuthentication(conn); err != nil {
			return err
		}
	}

	request, err := s.newRequest(conn)
	if err != nil {
		HandshakeErrors.Add(1)
		if errors.Is(err, model.ErrUnrecognizedAddrType) {
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
	if action.Action == appctlpb.EgressAction_PROXY {
		proxy := action.Proxy
		if proxy.GetSocks5Authentication().GetUser() != "" && proxy.GetSocks5Authentication().GetPassword() != "" {
			log.Debugf("Egress decision of socks5 request %v is %s to %v with user password authentication", request.Raw, action.Action.String(), util.MaybeDecorateIPv6(proxy.GetHost())+":"+strconv.Itoa(int(proxy.GetPort())))
		} else {
			log.Debugf("Egress decision of socks5 request %v is %s to %v with no authentication", request.Raw, action.Action.String(), util.MaybeDecorateIPv6(proxy.GetHost())+":"+strconv.Itoa(int(proxy.GetPort())))
		}
	} else {
		log.Debugf("Egress decision of socks5 request %v is %s", request.Raw, action.Action.String())
	}
	switch action.Action {
	case appctlpb.EgressAction_DIRECT:
		if err := s.handleRequest(context.Background(), request, conn); err != nil {
			return fmt.Errorf("handleRequest() failed: %w", err)
		}
	case appctlpb.EgressAction_PROXY:
		if action.Proxy == nil {
			return fmt.Errorf("egress action is PROXY but proxy info is unavailable")
		}
		return s.handleForwarding(request, conn, action.Proxy)
	case appctlpb.EgressAction_REJECT:
		return fmt.Errorf("connection is rejected by egress rules")
	}
	return nil
}

func (s *Server) handleForwarding(req *Request, conn net.Conn, proxy *appctlpb.EgressProxy) error {
	forwardHost := proxy.GetHost()
	forwardPort := proxy.GetPort()
	proxyConn, err := net.Dial("tcp", util.MaybeDecorateIPv6(forwardHost)+":"+strconv.Itoa(int(forwardPort)))
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
	return util.BidiCopy(conn, proxyConn)
}
