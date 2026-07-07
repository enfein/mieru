package socks5

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/egress"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/mathext"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

// ProxyDialer creates a new connection to proxy server.
type ProxyDialer interface {
	DialContext(ctx context.Context) (net.Conn, error)
}

// UDPAssociateMode controls how a SOCKS5 UDP associate request is relayed.
type UDPAssociateMode int

const (
	// UDPAssociateModePacketOverStream relays UDP packets through a streaming
	// connection using PacketOverStreamTunnel. This is the default
	// mode used by mieru client and server.
	UDPAssociateModePacketOverStream UDPAssociateMode = iota

	// UDPAssociateModeDatagram relays UDP packets with packeting connection.
	UDPAssociateModeDatagram
)

// Config is used to setup and configure a socks5 server.
type Config struct {
	// Proxy dialer used to carry socks5 traffic.
	ProxyDialer ProxyDialer

	// Authentication options.
	AuthOpts Auth

	// Handshake timeout to establish socks5 connection.
	// Use 0 or negative value to disable the timeout.
	HandshakeTimeout time.Duration

	// Use mieru proxy to carry socks5 traffic.
	UseProxy bool

	// Resolver can be provided to do custom name resolution.
	Resolver apicommon.DNSResolver

	// UDPAssociateMode controls how SOCKS5 UDP associate requests are relayed.
	// Zero value defaults to UDPAssociateModePacketOverStream.
	UDPAssociateMode UDPAssociateMode

	// ---- server only fields ----

	// Proxy users.
	Users map[string]*appctlpb.User

	// Egress configuration.
	Egress *appctlpb.Egress

	// Strategy to select IP address from DNS response.
	DualStackPreference common.DualStackPreference

	// Allow using socks5 to access resources served from loopback address.
	// This is for testing purpose.
	AllowLoopbackDestination bool

	// ---- client only fields ----

	// Do not wait for handshake to complete before sending payload.
	// This has no effect if UDP association is used.
	HandshakeNoWait bool
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
	if conf.UseProxy && conf.ProxyDialer == nil {
		return nil, fmt.Errorf("ProxyDialer must be set when proxy is enabled")
	}
	switch conf.UDPAssociateMode {
	case UDPAssociateModePacketOverStream, UDPAssociateModeDatagram:
	default:
		return nil, fmt.Errorf("unsupported UDP associate mode: %d", conf.UDPAssociateMode)
	}

	// Ensure HandshakeTimeout is not negative.
	conf.HandshakeTimeout = mathext.Max(conf.HandshakeTimeout, 0)

	// Ensure we have a DNS resolver.
	if conf.Resolver == nil {
		conf.Resolver = &net.Resolver{}
	}

	// Ensure Users is not nil.
	if conf.Users == nil {
		conf.Users = make(map[string]*appctlpb.User)
	}

	// Ensure EgressConfig is not nil.
	if conf.Egress == nil {
		conf.Egress = &appctlpb.Egress{}
	}

	s := &Server{
		config:      conf,
		chAccept:    make(chan net.Conn, 256),
		chAcceptErr: make(chan error, 1), // non-blocking
		die:         make(chan struct{}),
	}
	return s, nil
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
	conn = apicommon.WrapHierarchyConn(conn)
	defer conn.Close()
	log.Debugf("socks5 server starts to serve connection [%v - %v]", conn.LocalAddr(), conn.RemoteAddr())

	if s.config.UseProxy {
		return s.clientServeConn(conn)
	}
	return s.serverServeConn(conn)
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
			select {
			case s.chAcceptErr <- err:
			case <-s.die:
			}
			return
		}
		select {
		case s.chAccept <- conn:
		case <-s.die:
			conn.Close()
			return
		}
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("socks5 server accepted connection [%v - %v]", conn.LocalAddr(), conn.RemoteAddr())
		}
	}
}

// serverServeConn serves the incoming socks5 connection in proxy server.
// The connection is initiated by proxy client.
func (s *Server) serverServeConn(proxyConn net.Conn) error {
	if !s.config.AuthOpts.ClientSideAuthentication {
		if err := s.handleAuthentication(proxyConn); err != nil {
			return err
		}
	}

	ctx := context.Background()
	request, err := s.readRequest(proxyConn)
	if err != nil {
		HandshakeErrors.Add(1)
		if errors.Is(err, model.ErrUnrecognizedAddrType) {
			if err := model.WriteSocks5Response(proxyConn, constant.Socks5ReplyAddrTypeNotSupported, zeroBindAddr()); err != nil {
				return fmt.Errorf("failed to send reply for addrTypeNotSupported error: %w", err)
			}
		}
		return fmt.Errorf("failed to read destination address: %w", err)
	}

	egressInput := egress.Input{
		Protocol: appctlpb.ProxyProtocol_SOCKS5_PROXY_PROTOCOL,
		Data:     request.Raw,
	}
	userCtx := proxyConn.(apicommon.UserContext)
	userName := userCtx.UserName()
	if userName == "" {
		log.Debugf("Failed to determine user name from the connection")
	} else {
		log.Debugf("User %q initiated the connection", userName)
		egressInput.Env = map[string]string{
			"user": userName,
		}
	}
	action := s.FindAction(ctx, egressInput)
	if action.Action == appctlpb.EgressAction_PROXY {
		proxy := action.Proxy
		if proxy.GetSocks5Authentication().GetUser() != "" && proxy.GetSocks5Authentication().GetPassword() != "" {
			log.Debugf("Egress decision of socks5 request %v is %s to %v with user password authentication", request.Raw, action.Action.String(), common.MaybeDecorateIPv6(proxy.GetHost())+":"+strconv.Itoa(int(proxy.GetPort())))
		} else {
			log.Debugf("Egress decision of socks5 request %v is %s to %v with no authentication", request.Raw, action.Action.String(), common.MaybeDecorateIPv6(proxy.GetHost())+":"+strconv.Itoa(int(proxy.GetPort())))
		}
	} else {
		log.Debugf("Egress decision of socks5 request %v is %s", request.Raw, action.Action.String())
	}
	switch action.Action {
	case appctlpb.EgressAction_DIRECT:
		if err := s.handleRequest(ctx, request, proxyConn); err != nil {
			return fmt.Errorf("handleRequest() failed: %w", err)
		}
	case appctlpb.EgressAction_PROXY:
		if action.Proxy == nil {
			return fmt.Errorf("egress action is PROXY but proxy info is unavailable")
		}
		return s.handleForwarding(request, proxyConn, action.Proxy)
	case appctlpb.EgressAction_REJECT:
		RejectByRules.Add(1)
		if err := model.WriteSocks5Response(proxyConn, constant.Socks5ReplyNotAllowedByRuleSet, zeroBindAddr()); err != nil {
			return fmt.Errorf("failed to send reply for notAllowedByRuleSet error: %w", err)
		}
		return fmt.Errorf("connection is rejected by egress rules")
	}
	return nil
}

// clientServeConn serves the incoming socks5 connection in proxy client.
// The connection is initiated by the user.
func (s *Server) clientServeConn(userConn net.Conn) error {
	if s.config.AuthOpts.ClientSideAuthentication {
		if err := s.handleAuthentication(userConn); err != nil {
			return err
		}
	}

	dialCtx, dialCancel := context.WithCancel(context.Background())
	defer dialCancel()
	proxyConn, err := s.config.ProxyDialer.DialContext(dialCtx)
	if err != nil {
		return fmt.Errorf("proxy dialer DialContext() failed: %w", err)
	}

	if !s.config.AuthOpts.ClientSideAuthentication {
		if err := s.proxySocks5AuthReq(userConn, proxyConn); err != nil {
			HandshakeErrors.Add(1)
			proxyConn.Close()
			dialCancel()
			return err
		}
	}
	udpAssociateConn, pendingConnReq, err := s.proxySocks5ConnReq(userConn, proxyConn)
	if err != nil {
		HandshakeErrors.Add(1)
		proxyConn.Close()
		dialCancel()
		return err
	}
	if udpAssociateConn == nil {
		if s.config.HandshakeNoWait {
			return BidiCopySocks5(userConn, proxyConn, pendingConnReq)
		}
		return common.BidiCopy(userConn, proxyConn)
	}
	log.Debugf("UDP association is listening on %v", udpAssociateConn.LocalAddr())
	userConn.(apicommon.HierarchyConn).Add(udpAssociateConn)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		common.ReadAllAndDiscard(userConn)
		userConn.Close()
	}()
	err = BidiCopyUDP(udpAssociateConn, apicommon.NewPacketOverStreamTunnel(proxyConn))
	wg.Wait()
	return err
}
