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
	"strings"
	"sync"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/cipher"
	"github.com/enfein/mieru/v3/pkg/common"
	"github.com/enfein/mieru/v3/pkg/common/sockopts"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/mathext"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

const (
	idleUnderlayTickerInterval = 5 * time.Second
)

// Mux manages the sessions and underlays.
type Mux struct {
	// ---- common fields ----
	isClient    bool
	endpoints   []UnderlayProperties
	underlays   []Underlay
	chAccept    chan net.Conn
	chAcceptErr chan error
	used        bool
	done        chan struct{}
	mu          sync.Mutex
	cleaner     *time.Ticker

	// ---- client fields ----
	username        string
	password        []byte
	multiplexFactor int

	// ---- server fields ----
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
		isClient:    isClinet,
		underlays:   make([]Underlay, 0),
		chAccept:    make(chan net.Conn, sessionChanCapacity),
		chAcceptErr: make(chan error, 1), // non-blocking
		done:        make(chan struct{}),
		cleaner:     time.NewTicker(idleUnderlayTickerInterval),
	}

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
		for _, underlay := range m.underlays {
			if udpUnderlay, ok := underlay.(*PacketUnderlay); ok {
				udpUnderlay.users = m.users
			}
		}
	}
	return m
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
				for _, p := range new {
					go m.acceptUnderlayLoop(context.Background(), p)
				}
				m.endpoints = endpoints
			}
		} else {
			m.endpoints = new
		}
	}
	log.Infof("Mux now has %d endpoints", len(m.endpoints))
	return m
}

func (m *Mux) Accept() (net.Conn, error) {
	select {
	case err := <-m.chAcceptErr:
		return nil, err
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
	for _, p := range m.endpoints {
		go m.acceptUnderlayLoop(context.Background(), p)
	}
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

// DialContextWithConn returns a network connection for the client to consume.
// The connection is a session established from a underlay constructed from
// the given connection.
func (m *Mux) DialContextWithConn(ctx context.Context, conn net.Conn) (net.Conn, error) {
	if !m.isClient {
		return nil, stderror.ErrInvalidOperation
	}
	if len(m.password) == 0 {
		return nil, fmt.Errorf("client password is not set")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.used = true

	m.cleanUnderlay(true)
	block, err := cipher.BlockCipherFromPassword(m.password, false)
	if err != nil {
		return nil, fmt.Errorf("cipher.BlockCipherFromPassword() failed: %v", err)
	}
	block.SetBlockContext(cipher.BlockContext{
		UserName: m.username,
	})
	// The MTU value is not used by TCP connection.
	underlay, err := NewStreamUnderlayWithConn(conn, common.DefaultMTU, block)
	if err != nil {
		return nil, fmt.Errorf("NewTCPUnderlayWithConn() failed: %v", err)
	}
	session := NewSession(mrand.Uint32(), true, underlay.MTU(), m.users)
	if err := underlay.AddSession(session, nil); err != nil {
		return nil, fmt.Errorf("AddSession() failed: %v", err)
	}
	return session, nil
}

// ExportSessionInfoTable returns multiple lines of strings that display
// session info in a table format.
func (m *Mux) ExportSessionInfoTable() []string {
	header := SessionInfo{
		ID:         "Session ID",
		Protocol:   "Protocol",
		LocalAddr:  "Local",
		RemoteAddr: "Remote",
		State:      "State",
		RecvQBuf:   "Recv Q+Buf",
		SendQBuf:   "Send Q+Buf",
		LastRecv:   "Last Recv",
		LastSend:   "Last Send",
	}
	info := []SessionInfo{header}
	m.mu.Lock()
	for _, underlay := range m.underlays {
		info = append(info, underlay.Sessions()...)
	}
	m.mu.Unlock()

	var idLen, protocolLen, localAddrLen, remoteAddrLen, stateLen, recvQLen, sendQLen, lastRecvLen, lastSendLen int
	for _, si := range info {
		idLen = mathext.Max(idLen, len(si.ID))
		protocolLen = mathext.Max(protocolLen, len(si.Protocol))
		localAddrLen = mathext.Max(localAddrLen, len(si.LocalAddr))
		remoteAddrLen = mathext.Max(remoteAddrLen, len(si.RemoteAddr))
		stateLen = mathext.Max(stateLen, len(si.State))
		recvQLen = mathext.Max(recvQLen, len(si.RecvQBuf))
		sendQLen = mathext.Max(sendQLen, len(si.SendQBuf))
		lastRecvLen = mathext.Max(lastRecvLen, len(si.LastRecv))
		lastSendLen = mathext.Max(lastSendLen, len(si.LastSend))
	}
	res := make([]string, 0)
	delim := "  "
	for _, si := range info {
		line := make([]string, 0)
		line = append(line, fmt.Sprintf("%-"+fmt.Sprintf("%d", idLen)+"s", si.ID))
		line = append(line, fmt.Sprintf("%-"+fmt.Sprintf("%d", protocolLen)+"s", si.Protocol))
		line = append(line, fmt.Sprintf("%-"+fmt.Sprintf("%d", localAddrLen)+"s", si.LocalAddr))
		line = append(line, fmt.Sprintf("%-"+fmt.Sprintf("%d", remoteAddrLen)+"s", si.RemoteAddr))
		line = append(line, fmt.Sprintf("%-"+fmt.Sprintf("%d", stateLen)+"s", si.State))
		line = append(line, fmt.Sprintf("%-"+fmt.Sprintf("%d", recvQLen)+"s", si.RecvQBuf))
		line = append(line, fmt.Sprintf("%-"+fmt.Sprintf("%d", sendQLen)+"s", si.SendQBuf))
		line = append(line, fmt.Sprintf("%-"+fmt.Sprintf("%d", lastRecvLen)+"s", si.LastRecv))
		line = append(line, fmt.Sprintf("%-"+fmt.Sprintf("%d", lastSendLen)+"s", si.LastSend))
		res = append(res, strings.Join(line, delim))
	}
	return res
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

func (m *Mux) acceptUnderlayLoop(ctx context.Context, properties UnderlayProperties) {
	laddr := properties.LocalAddr().String()
	if laddr == "" {
		m.chAcceptErr <- fmt.Errorf("underlay local address is empty")
		return
	}

	network := properties.LocalAddr().Network()
	switch network {
	case "tcp", "tcp4", "tcp6":
		listenConfig := sockopts.ListenConfigWithControls()
		rawListener, err := listenConfig.Listen(ctx, network, laddr)
		if err != nil {
			m.chAcceptErr <- fmt.Errorf("Listen() failed: %w", err)
			return
		}
		log.Infof("Mux is listening to endpoint %s %s", network, laddr)

		acceptLoopDone := ctx.Done()
		for {
			select {
			case <-acceptLoopDone:
				return
			default:
				underlay, err := m.acceptTCPUnderlay(rawListener, properties)
				if err != nil {
					m.chAcceptErr <- err
					return
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

				go func(ctx context.Context, underlay Underlay) {
					err := underlay.RunEventLoop(ctx)
					if err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
						log.Debugf("%v RunEventLoop(): %v", underlay, err)
					}
					underlay.Close()
				}(ctx, underlay)

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
		}
	case "udp", "udp4", "udp6":
		conn, err := net.ListenUDP(network, properties.LocalAddr().(*net.UDPAddr))
		if err != nil {
			m.chAcceptErr <- fmt.Errorf("ListenUDP() failed: %w", err)
			return
		}
		if err := sockopts.ApplyUDPControls(conn); err != nil {
			m.chAcceptErr <- fmt.Errorf("ApplyUDPControls() failed: %w", err)
			return
		}
		log.Infof("Mux is listening to endpoint %s %s", network, laddr)
		underlay := &PacketUnderlay{
			baseUnderlay:      *newBaseUnderlay(false, properties.MTU()),
			conn:              conn,
			idleSessionTicker: time.NewTicker(idleSessionTickerInterval),
			users:             m.users,
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

		go func(ctx context.Context, underlay Underlay) {
			err := underlay.RunEventLoop(ctx)
			if err != nil && !stderror.IsEOF(err) && !stderror.IsClosed(err) {
				log.Debugf("%v RunEventLoop(): %v", underlay, err)
			}
			underlay.Close()
		}(ctx, underlay)

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
		m.chAcceptErr <- fmt.Errorf("unsupported underlay network type %q", network)
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
		baseUnderlay: *newBaseUnderlay(false, mtu),
		conn:         rawConn,
		candidates:   blocks,
		users:        users,
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
		underlay, err = NewStreamUnderlay(ctx, p.RemoteAddr().Network(), "", p.RemoteAddr().String(), p.MTU(), block)
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
		underlay, err = NewPacketUnderlay(ctx, p.RemoteAddr().Network(), "", p.RemoteAddr().String(), p.MTU(), block)
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
		err := underlay.RunEventLoop(ctx)
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
func (m *Mux) cleanUnderlay(alsoDisableIdleUnderlay bool) {
	remaining := make([]Underlay, 0)
	disable := 0
	close := 0
	for _, underlay := range m.underlays {
		select {
		case <-underlay.Done():
		default:
			if alsoDisableIdleUnderlay && underlay.SessionCount() == 0 {
				if underlay.Scheduler().TryDisable() {
					disable++
				}
			}
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
