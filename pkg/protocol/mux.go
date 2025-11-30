// Copyright (C) 2023  mieru authors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package protocol

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	apicommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/mathext"
	"github.com/enfein/mieru/v3/pkg/sockopts"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

const (
	underlayCleanInterval = 5 * time.Second
)

// Mux manages the sessions and underlays.
type Mux struct {
	// ---- common fields ----
	isClient              bool
	endpoints             []UnderlayProperties
	underlays             []Underlay
	dialer                apicommon.Dialer
	packetDialer          apicommon.PacketDialer
	resolver              apicommon.DNSResolver
	clientDNSConfig       *apicommon.ClientDNSConfig
	streamListenerFactory apicommon.StreamListenerFactory
	packetListenerFactory apicommon.PacketListenerFactory
	chAccept              chan net.Conn
	acceptHasErr          atomic.Bool
	acceptErr             chan error // this channel is closed when accept has error
	used                  bool
	done                  chan struct{}
	ctx                   context.Context    // mux master context
	ctxCancelFunc         context.CancelFunc // function to cancel master context when mux is closed
	mu                    sync.Mutex
	cleaner               *time.Ticker

	// ---- client only fields ----
	username        string
	password        []byte
	multiplexFactor int

	// ---- server only fields ----
	users map[string]*appctlpb.User
}

var _ net.Listener = &Mux{}

// NewMux creates a new mieru v2 multiplex controller.
func NewMux(isClinet bool) *Mux {
	if isClinet {
		log.Infof("Initializing client multiplexer")
	} else {
		log.Infof("Initializing server multiplexer")
	}
	mux := &Mux{
		isClient:              isClinet,
		underlays:             make([]Underlay, 0),
		dialer:                &net.Dialer{Timeout: 10 * time.Second, Control: sockopts.DefaultDialerControl()},
		packetDialer:          common.UDPDialer{Control: sockopts.DefaultDialerControl()},
		resolver:              &net.Resolver{},
		clientDNSConfig:       &apicommon.ClientDNSConfig{},
		streamListenerFactory: &net.ListenConfig{Control: sockopts.DefaultListenerControl()},
		packetListenerFactory: &net.ListenConfig{Control: sockopts.DefaultListenerControl()},
		chAccept:              make(chan net.Conn, sessionChanCapacity),
		acceptErr:             make(chan error),
		done:                  make(chan struct{}),
		cleaner:               time.NewTicker(underlayCleanInterval),
	}
	mux.ctx, mux.ctxCancelFunc = context.WithCancel(context.Background())

	// Run maintenance tasks in the background.
	go func() {
		for {
			select {
			case <-mux.cleaner.C:
				mux.mu.Lock()
				if isClinet {
					mux.cleanUnderlay(true)
				} else {
					mux.cleanUnderlay(false)
				}
				mux.mu.Unlock()
			case <-mux.done:
				mux.cleaner.Stop()
				return
			}
		}
	}()
	return mux
}

// SetEndpoints updates the endpoints that mux is listening to.
// If mux is started and new endpoints are added, mux also starts
// to listen to those new endpoints. In that case, old endpoints
// are not impacted.
func (m *Mux) SetEndpoints(endpoints []UnderlayProperties) *Mux {
	m.mu.Lock()
	defer m.mu.Unlock()
	new := m.newEndpoints(m.endpoints, endpoints)
	if len(new) > 0 {
		if m.used {
			select {
			case <-m.done:
				log.Infof("Unable to add new endpoint after multiplexer is closed")
			default:
				var wg sync.WaitGroup
				for _, p := range new {
					wg.Add(1)
					go m.acceptUnderlayLoop(m.ctx, p, &wg)
				}
				wg.Wait()
				m.endpoints = endpoints
			}
		} else {
			m.endpoints = new
		}
	}
	log.Infof("Mux now has %d endpoints", len(m.endpoints))
	return m
}

// SetDialer updates the dialer used by the mux.
func (m *Mux) SetDialer(dialer apicommon.Dialer) *Mux {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dialer = dialer
	log.Infof("Mux dialer has been updated")
	return m
}

// SetPacketDialer updates the packet dialer used by the mux.
func (m *Mux) SetPacketDialer(packetDialer apicommon.PacketDialer) *Mux {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.packetDialer = packetDialer
	log.Infof("Mux packet dialer has been updated")
	return m
}

// SetResolver updates the DNS resolver used by the mux.
func (m *Mux) SetResolver(resolver apicommon.DNSResolver) *Mux {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolver = resolver
	log.Infof("Mux DNS resolver has been updated")
	return m
}

// SetClientDNSConfig updates the client DNS configuration used by the mux.
func (m *Mux) SetClientDNSConfig(clientDNSConfig *apicommon.ClientDNSConfig) *Mux {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientDNSConfig = clientDNSConfig
	log.Infof("Mux client DNS configuration has been updated")
	return m
}

// SetStreamListenerFactory updates the stream oriented network listener factory used by the mux.
func (m *Mux) SetStreamListenerFactory(listenerFactory apicommon.StreamListenerFactory) *Mux {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streamListenerFactory = listenerFactory
	log.Infof("Mux stream listener factory has been updated")
	return m
}

// SetPacketListenerFactory updates the packet oriented network listener factory used by the mux.
func (m *Mux) SetPacketListenerFactory(listenerFactory apicommon.PacketListenerFactory) *Mux {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.packetListenerFactory = listenerFactory
	log.Infof("Mux packet listener factory has been updated")
	return m
}

// SetClientUserNamePassword panics if the mux is already started.
func (m *Mux) SetClientUserNamePassword(username string, password []byte) *Mux {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isClient {
		panic("Can't set client password in server mux")
	}
	if m.used {
		panic("Can't set client password after mux is used")
	}
	m.username = username
	m.password = password
	return m
}

// SetClientMultiplexFactor panics if the mux is already started.
func (m *Mux) SetClientMultiplexFactor(n int) *Mux {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isClient {
		panic("Can't set multiplex factor in server mux")
	}
	if m.used {
		panic("Can't set multiplex factor after mux is used")
	}
	m.multiplexFactor = mathext.Max(n, 0)
	log.Infof("Mux multiplexing factor is set to %d", m.multiplexFactor)
	return m
}

// SetServerUsers updates the registered users, even if mux is already started.
func (m *Mux) SetServerUsers(users map[string]*appctlpb.User) *Mux {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isClient {
		panic("Can't set server users in client mux")
	}
	m.users = users
	if m.used {
		// Update the users in UDPUnderlay.
		// Don't update TCPUnderlay and Session, so existing connections still work.
		// Newly established TCPUnderlay automatically pick up the updated users.
		for _, underlay := range m.underlays {
			if udpUnderlay, ok := underlay.(*PacketUnderlay); ok {
				udpUnderlay.users = m.users
			}
		}
	}
	return m
}

func (m *Mux) Accept() (net.Conn, error) {
	select {
	case <-m.acceptErr:
		return nil, io.ErrClosedPipe
	case conn := <-m.chAccept:
		return conn, nil
	case <-m.done:
		return nil, io.EOF
	}
}

func (m *Mux) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-m.done:
		return nil
	default:
	}

	if m.isClient {
		log.Infof("Closing client multiplexer")
	} else {
		log.Infof("Closing server multiplexer")
	}
	for _, underlay := range m.underlays {
		underlay.Close()
	}
	m.underlays = make([]Underlay, 0)
	m.ctxCancelFunc()
	close(m.done)
	return nil
}

// Addr is not supported by Mux.
func (m *Mux) Addr() net.Addr {
	return common.NilNetAddr()
}

// Start listens on all the server addresses for incoming connections.
// Call this method in client results in an error.
// This method doesn't block.
func (m *Mux) Start() error {
	if m.isClient {
		return stderror.ErrInvalidOperation
	}
	if len(m.users) == 0 {
		return fmt.Errorf("no user found")
	}
	if len(m.endpoints) == 0 {
		return fmt.Errorf("no server listening endpoint found")
	}
	for _, p := range m.endpoints {
		if common.IsNilNetAddr(p.LocalAddr()) {
			return fmt.Errorf("endpoint local address is not set")
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.used = true
	var wg sync.WaitGroup
	for _, p := range m.endpoints {
		wg.Add(1)
		go m.acceptUnderlayLoop(m.ctx, p, &wg)
	}
	wg.Wait()
	return nil
}

// DialContext returns a network connection for the client to consume.
// The connection may be a session established from an existing underlay.
func (m *Mux) DialContext(ctx context.Context) (net.Conn, error) {
	if !m.isClient {
		return nil, stderror.ErrInvalidOperation
	}
	if len(m.password) == 0 {
		return nil, fmt.Errorf("client password is not set")
	}
	if len(m.endpoints) == 0 {
		return nil, fmt.Errorf("no server listening endpoint found")
	}
	for _, p := range m.endpoints {
		if common.IsNilNetAddr(p.RemoteAddr()) {
			return nil, fmt.Errorf("endpoint remote address is not set")
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.used = true
	var err error

	// Try to find a underlay for the session.
	m.cleanUnderlay(true)
	underlay := m.maybePickExistingUnderlay()
	if underlay == nil {
		underlay, err = m.newUnderlay(ctx)
		if err != nil {
			return nil, err
		}
		log.Debugf("Created new underlay %v", underlay)
	} else {
		log.Debugf("Reusing existing underlay %v", underlay)
	}

	if ok := underlay.Scheduler().IncPending(); !ok {
		// This underlay can't be used. Create a new one.
		underlay, err = m.newUnderlay(ctx)
		if err != nil {
			return nil, err
		}
		log.Debugf("Created yet another new underlay %v", underlay)
		underlay.Scheduler().IncPending()
	}
	defer func() {
		underlay.Scheduler().DecPending()
	}()
	session := NewSession(mrand.Uint32(), true, underlay.MTU(), m.users)
	if err := underlay.AddSession(session, nil); err != nil {
		return nil, fmt.Errorf("AddSession() failed: %v", err)
	}
	return session, nil
}

func (m *Mux) ExportSessionInfoList() *appctlpb.SessionInfoList {
	items := make([]*appctlpb.SessionInfo, 0)
	m.mu.Lock()
	for _, underlay := range m.underlays {
		items = append(items, underlay.SessionInfos()...)
	}
	m.mu.Unlock()
	return &appctlpb.SessionInfoList{Items: items}
}

func (m *Mux) newEndpoints(old, new []UnderlayProperties) []UnderlayProperties {
	newEndpoints := []UnderlayProperties{}

	for _, n := range new {
		found := false
		for _, o := range old {
			if reflect.DeepEqual(n, o) {
				found = true
				break
			}
		}
		if !found {
			newEndpoints = append(newEndpoints, n)
		}
	}

	return newEndpoints
}

func (m *Mux) acceptUnderlayLoop(ctx context.Context, properties UnderlayProperties, wg *sync.WaitGroup) {
	laddr := properties.LocalAddr().String()
	if laddr == "" {
		log.Errorf("Underlay local address is empty")
		if m.acceptHasErr.CompareAndSwap(false, true) {
			close(m.acceptErr)
		}
		wg.Done()
		return
	}

	network := properties.LocalAddr().Network()
	switch network {
	case "tcp", "tcp4", "tcp6":
		tcpAddr, err := apicommon.ResolveTCPAddr(ctx, m.resolver, "tcp", laddr)
		if err != nil {
			log.Errorf("ResolveTCPAddr() failed: %v", err)
			if m.acceptHasErr.CompareAndSwap(false, true) {
				close(m.acceptErr)
			}
			wg.Done()
			return
		}
		rawListener, err := m.streamListenerFactory.Listen(ctx, tcpAddr.Network(), tcpAddr.String())
		if err != nil {
			log.Errorf("Listen() failed: %v", err)
			if m.acceptHasErr.CompareAndSwap(false, true) {
				close(m.acceptErr)
			}
			wg.Done()
			return
		}
		wg.Done()
		log.Infof("Mux is listening to endpoint %s %s", network, laddr)

		// Close the rawListener if the master context is canceled.
		// This can break the forever loop below.
		go func(ctx context.Context, l net.Listener) {
			<-ctx.Done()
			log.Infof("Closing TCPListener %v", rawListener.Addr())
			rawListener.Close()
		}(ctx, rawListener)

		for {
			// A new underlay should be established.
			underlay, err := m.acceptTCPUnderlay(rawListener, properties)
			if err != nil {
				log.Debugf("%v", err)
				break
			}
			log.Debugf("Created new server underlay %v", underlay)
			m.mu.Lock()
			m.underlays = append(m.underlays, underlay)
			m.cleanUnderlay(false)
			m.mu.Unlock()
			UnderlayPassiveOpens.Add(1)
			currEst := UnderlayCurrEstablished.Add(1)
			maxConn := UnderlayMaxConn.Load()
			if currEst > maxConn {
				UnderlayMaxConn.Store(currEst)
			}

			// Run underlay event loop.
			go func(ctx context.Context, underlay Underlay) {
				err := underlay.RunEventLoop(ctx)
				if err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
					log.Debugf("%v RunEventLoop(): %v", underlay, err)
				}
				underlay.Close()
			}(ctx, underlay)

			// Accept sessions from the underlay.
			go func(ctx context.Context, underlay Underlay) {
				for {
					conn, err := underlay.Accept()
					if err != nil {
						if !stderror.IsEOF(err) && !stderror.IsClosed(err) {
							log.Debugf("%v Accept(): %v", underlay, err)
						}
						break
					}
					select {
					case m.chAccept <- conn:
					case <-ctx.Done():
						return
					}
				}
			}(ctx, underlay)
		}
	case "udp", "udp4", "udp6":
		udpAddr, err := apicommon.ResolveUDPAddr(ctx, m.resolver, "udp", laddr)
		if err != nil {
			log.Errorf("ResolveUDPAddr() failed: %v", err)
			if m.acceptHasErr.CompareAndSwap(false, true) {
				close(m.acceptErr)
			}
			wg.Done()
			return
		}
		conn, err := m.packetListenerFactory.ListenPacket(ctx, udpAddr.Network(), udpAddr.String())
		if err != nil {
			log.Errorf("ListenPacket() failed: %v", err)
			if m.acceptHasErr.CompareAndSwap(false, true) {
				close(m.acceptErr)
			}
			wg.Done()
			return
		}
		wg.Done()
		log.Infof("Mux is listening to endpoint %s %s", network, laddr)

		underlay := &PacketUnderlay{
			baseUnderlay:       *newBaseUnderlay(false, properties.MTU()),
			conn:               conn,
			sessionCleanTicker: time.NewTicker(sessionCleanInterval),
			users:              m.users,
		}
		log.Infof("Created new server underlay %v", underlay)
		m.mu.Lock()
		m.underlays = append(m.underlays, underlay)
		m.cleanUnderlay(false)
		m.mu.Unlock()
		UnderlayPassiveOpens.Add(1)
		currEst := UnderlayCurrEstablished.Add(1)
		maxConn := UnderlayMaxConn.Load()
		if currEst > maxConn {
			UnderlayMaxConn.Store(currEst)
		}

		// Run underlay event loop.
		go func(ctx context.Context, underlay Underlay) {
			err := underlay.RunEventLoop(ctx)
			if err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
				log.Debugf("%v RunEventLoop(): %v", underlay, err)
			}
			underlay.Close()
		}(ctx, underlay)

		// Accept sessions from the underlay.
		go func(ctx context.Context, underlay Underlay) {
			for {
				conn, err := underlay.Accept()
				if err != nil {
					if !stderror.IsEOF(err) && !stderror.IsClosed(err) {
						log.Debugf("%v Accept(): %v", underlay, err)
					}
					break
				}
				select {
				case m.chAccept <- conn:
				case <-ctx.Done():
					return
				}
			}
		}(ctx, underlay)
	default:
		log.Errorf("Unsupported underlay network type %q", network)
		if m.acceptHasErr.CompareAndSwap(false, true) {
			close(m.acceptErr)
		}
		wg.Done()
	}
}

func (m *Mux) acceptTCPUnderlay(rawListener net.Listener, properties UnderlayProperties) (Underlay, error) {
	rawConn, err := rawListener.Accept()
	if err != nil {
		return nil, fmt.Errorf("Accept() underlay failed: %w", err)
	}
	return m.serverWrapTCPConn(rawConn, properties.MTU(), m.users), nil
}

func (m *Mux) serverWrapTCPConn(rawConn net.Conn, mtu int, users map[string]*appctlpb.User) Underlay {
	var err error
	var blocks []cipher.BlockCipher
	for _, user := range users {
		var password []byte
		password, err = hex.DecodeString(user.GetHashedPassword())
		if err != nil {
			log.Debugf("Unable to decode hashed password %q from user %q", user.GetHashedPassword(), user.GetName())
			continue
		}
		if len(password) == 0 {
			password = cipher.HashPassword([]byte(user.GetPassword()), []byte(user.GetName()))
		}
		blocksFromUser, err := cipher.BlockCipherListFromPassword(password, false)
		if err != nil {
			log.Debugf("Unable to create block cipher of user %q", user.GetName())
			continue
		}
		for _, block := range blocksFromUser {
			block.SetBlockContext(cipher.BlockContext{
				UserName: user.GetName(),
			})
		}
		blocks = append(blocks, blocksFromUser...)
	}
	return &StreamUnderlay{
		baseUnderlay:       *newBaseUnderlay(false, mtu),
		conn:               rawConn,
		candidates:         blocks,
		sessionCleanTicker: time.NewTicker(sessionCleanInterval),
		users:              users,
	}
}

// newUnderlay returns a new underlay.
// This method MUST be called only when holding the mu lock.
func (m *Mux) newUnderlay(ctx context.Context) (Underlay, error) {
	var underlay Underlay
	i := mrand.Intn(len(m.endpoints))
	p := m.endpoints[i]
	switch p.TransportProtocol() {
	case common.StreamTransport:
		block, err := cipher.BlockCipherFromPassword(m.password, false)
		if err != nil {
			return nil, fmt.Errorf("cipher.BlockCipherFromPassword() failed: %v", err)
		}
		block.SetBlockContext(cipher.BlockContext{
			UserName: m.username,
		})
		underlay, err = NewStreamUnderlay(ctx, m.dialer, m.resolver, m.clientDNSConfig, p.RemoteAddr().Network(), p.RemoteAddr().String(), p.MTU(), block)
		if err != nil {
			return nil, fmt.Errorf("NewTCPUnderlay() failed: %v", err)
		}
	case common.PacketTransport:
		block, err := cipher.BlockCipherFromPassword(m.password, true)
		if err != nil {
			return nil, fmt.Errorf("cipher.BlockCipherFromPassword() failed: %v", err)
		}
		block.SetBlockContext(cipher.BlockContext{
			UserName: m.username,
		})
		underlay, err = NewPacketUnderlay(ctx, m.packetDialer, m.resolver, p.RemoteAddr().Network(), p.RemoteAddr().String(), p.MTU(), block)
		if err != nil {
			return nil, fmt.Errorf("NewUDPUnderlay() failed: %v", err)
		}
	default:
		return nil, fmt.Errorf("unsupport transport protocol %v", p.TransportProtocol())
	}
	m.underlays = append(m.underlays, underlay)
	UnderlayActiveOpens.Add(1)
	currEst := UnderlayCurrEstablished.Add(1)
	maxConn := UnderlayMaxConn.Load()
	if currEst > maxConn {
		UnderlayMaxConn.Store(currEst)
	}
	go func() {
		// This is a long running loop, detach from client dial context.
		err := underlay.RunEventLoop(context.Background())
		if err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
			log.Debugf("%v RunEventLoop(): %v", underlay, err)
		}
		underlay.Close()
	}()
	return underlay, nil
}

// maybePickExistingUnderlay returns either an existing underlay that
// can be used by a session, or nil. In the later case a new underlay
// should be created.
// This method MUST be called only when holding the mu lock.
func (m *Mux) maybePickExistingUnderlay() Underlay {
	active := make([]Underlay, 0)
	for _, underlay := range m.underlays {
		select {
		case <-underlay.Done():
		default:
			if !underlay.Scheduler().IsDisabled() {
				active = append(active, underlay)
			}
		}
	}

	if m.multiplexFactor > 0 {
		reuseUnderlayFactor := len(active) * m.multiplexFactor
		n := mrand.Intn(reuseUnderlayFactor + 1)
		if n < reuseUnderlayFactor {
			return active[n/m.multiplexFactor]
		}
	}
	return nil
}

// cleanUnderlay removes closed underlays.
// This method MUST be called only when holding the mu lock.
func (m *Mux) cleanUnderlay(alsoDisableIdleOrOverloadUnderlay bool) {
	remaining := make([]Underlay, 0)
	disable := 0
	close := 0
	for _, underlay := range m.underlays {
		select {
		case <-underlay.Done():
		default:
			if alsoDisableIdleOrOverloadUnderlay {
				// Disable idle underlay.
				if underlay.SessionCount() == 0 {
					if underlay.Scheduler().TryDisableIdle() {
						disable++
					}
				}

				// Disable overloaded underlay.
				// If multiplexFactor is 1, the limit is 1 GiB.
				var trafficVolumeLimit int64 = 512 * 1024 * 1024 << m.multiplexFactor
				if underlay.Scheduler().DisableTime().IsZero() && (underlay.InBytes() > trafficVolumeLimit || underlay.OutBytes() > trafficVolumeLimit) {
					underlay.Scheduler().SetRemainingTime(0)
					disable++
				}
			}

			// Close idle underlay.
			if underlay.SessionCount() == 0 && underlay.Scheduler().Idle() {
				underlay.Close()
				close++
			} else {
				remaining = append(remaining, underlay)
			}
		}
	}
	m.underlays = remaining
	if disable > 0 {
		log.Debugf("Mux disabled scheduling from %d underlays", disable)
	}
	if close > 0 {
		log.Debugf("Mux cleaned %d underlays", close)
	}
}
