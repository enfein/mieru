// Copyright (C) 2021  mieru authors
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

package udpsession

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/recording"
	"github.com/enfein/mieru/pkg/replay"
	"github.com/enfein/mieru/pkg/testtool"
	"google.golang.org/protobuf/proto"
)

func runCloseWaitClient(t *testing.T, laddr, raddr string, username, password []byte, clientReq, serverResp *testtool.ReplayRecord) error {
	hashedPassword := cipher.HashPassword(password, username)
	block, err := cipher.BlockCipherFromPassword(hashedPassword, true)
	if err != nil {
		return fmt.Errorf("cipher.BlockCipherFromPassword() failed: %w", err)
	}
	sess, err := DialWithOptions(context.Background(), "udp", laddr, raddr, block)
	if err != nil {
		return fmt.Errorf("DialWithOptions() failed: %w", err)
	}
	defer sess.Close()
	t.Logf("[%s] client is running on %v", time.Now().Format(testtool.TimeLayout), laddr)

	respBuf := make([]byte, 1500)
	data := testtool.TestHelperGenRot13Input(1024)

	// Send client request to server.
	sess.startRecording()
	if _, err = sess.Write(data); err != nil {
		return fmt.Errorf("Write() failed: %w", err)
	}
	sess.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Record client request if needed.
	if clientReq != nil {
		found := false
		for i := 0; i < 10; i++ {
			if sess.recordedPackets.Size() == 0 {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			records := sess.recordedPackets.Export()
			for j := len(records) - 1; j >= 0; j-- {
				if records[j].Direction() == recording.Egress && len(records[j].Data()) >= 1024 {
					clientReq.Data = records[j].Data()
					found = true
					break
				}
			}
			if found {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !found {
			return fmt.Errorf("egress recording is not found")
		}
		close(clientReq.Ready)
	}

	// Get and verify server response.
	size, err := sess.Read(respBuf)
	if err != nil {
		return fmt.Errorf("Read() failed: %w", err)
	}
	revert, err := testtool.TestHelperRot13(respBuf[:size])
	if err != nil {
		return fmt.Errorf("testtool.TestHelperRot13() failed: %w", err)
	}
	if !bytes.Equal(data, revert) {
		return fmt.Errorf("verification failed")
	}

	// Record server response if needed.
	if serverResp != nil {
		found := false
		for i := 0; i < 10; i++ {
			if sess.recordedPackets.Size() == 0 {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			records := sess.recordedPackets.Export()
			for j := len(records) - 1; j >= 0; j-- {
				if records[j].Direction() == recording.Ingress && len(records[j].Data()) >= 1024 {
					serverResp.Data = records[j].Data()
					found = true
					break
				}
			}
			if found {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !found {
			return fmt.Errorf("ingress recording is not found")
		}
		close(serverResp.Ready)
		<-serverResp.Finish
	}

	return nil
}

// TestReplayServerResponseToClient creates 1 client, 1 server and 1 monitor.
// The monitor records the traffic between the client and server, then replays
// the server's response back to the client. The client should drop the replay
// packet before processing it.
func TestReplayServerResponseToClient(t *testing.T) {
	serverPort, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	clientPort, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	attackPort, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	serverAddr := fmt.Sprintf("127.0.0.1:%d", serverPort)
	clientAddr := fmt.Sprintf("127.0.0.1:%d", clientPort)
	attackAddr := fmt.Sprintf("127.0.0.1:%d", attackPort)
	clientUDPAddr, _ := net.ResolveUDPAddr("udp", clientAddr)
	attackUDPAddr, _ := net.ResolveUDPAddr("udp", attackAddr)
	users := map[string]*appctlpb.User{
		"danchaofan": {
			Name:     proto.String("danchaofan"),
			Password: proto.String("19501125"),
		},
	}

	server, err := ListenWithOptions(serverAddr, users)
	if err != nil {
		t.Fatalf("ListenWithOptions() failed: %v", err)
	}

	go func() {
		for {
			s, err := server.Accept()
			if err != nil {
				return
			} else {
				t.Logf("[%s] accepting new connection from %v", time.Now().Format(testtool.TimeLayout), s.RemoteAddr())
				go func() {
					if err = testtool.TestHelperServeConn(s); err != nil {
						return
					}
				}()
			}
		}
	}()
	time.Sleep(1 * time.Second)

	serverResp := &testtool.ReplayRecord{
		Ready:  make(chan struct{}),
		Finish: make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := runCloseWaitClient(t, clientAddr, serverAddr, []byte("danchaofan"), []byte("19501125"), nil, serverResp); err != nil {
			t.Errorf("[%s] maoanying failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
		wg.Done()
	}()
	<-serverResp.Ready

	// Get the current UDP error counter.
	errCnt := UDPInErrors.Load()
	t.Logf("UDPInErrors value before replay: %d", UDPInErrors.Load())

	// Replay the server's response to the client.
	replayConn, err := net.ListenUDP("udp", attackUDPAddr)
	if err != nil {
		t.Fatalf("net.ListenUDP() on %v failed: %v", attackUDPAddr, err)
	}
	defer replayConn.Close()
	replayConn.WriteTo(serverResp.Data, clientUDPAddr)

	// The UDP error counter should increase.
	increased := false
	for i := 0; i < 50; i++ {
		if UDPInErrors.Load() > errCnt {
			increased = true
			break
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Logf("UDPInErrors value after replay: %d", UDPInErrors.Load())
	if !increased {
		t.Errorf("UDPInErrors value %d is not changed after replay server response", UDPInErrors.Load())
	}

	close(serverResp.Finish)
	wg.Wait()
	server.Close()
	replayCache.Clear()
	time.Sleep(1 * time.Second) // Wait for resources to be released.
}

// TestReplayClientRequestToServer creates 1 client, 1 server and 1 monitor.
// The monitor records the traffic between the client and server, then replays
// the client's request to the server. The monitor should not get any response
// from the server.
func TestReplayClientRequestToServer(t *testing.T) {
	serverPort, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	clientPort, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	attackPort, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	serverAddr := fmt.Sprintf("127.0.0.1:%d", serverPort)
	clientAddr := fmt.Sprintf("127.0.0.1:%d", clientPort)
	attackAddr := fmt.Sprintf("127.0.0.1:%d", attackPort)
	serverUDPAddr, _ := net.ResolveUDPAddr("udp", serverAddr)
	attackUDPAddr, _ := net.ResolveUDPAddr("udp", attackAddr)
	users := map[string]*appctlpb.User{
		"danchaofan": {
			Name:     proto.String("danchaofan"),
			Password: proto.String("19501125"),
		},
	}

	server, err := ListenWithOptions(serverAddr, users)
	if err != nil {
		t.Fatalf("ListenWithOptions() failed: %v", err)
	}

	go func() {
		for {
			s, err := server.Accept()
			if err != nil {
				return
			} else {
				t.Logf("[%s] accepting new connection from %v", time.Now().Format(testtool.TimeLayout), s.RemoteAddr())
				go func() {
					if err = testtool.TestHelperServeConn(s); err != nil {
						return
					}
				}()
			}
		}
	}()
	time.Sleep(1 * time.Second)

	clientReq := &testtool.ReplayRecord{
		Ready:  make(chan struct{}),
		Finish: make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := runCloseWaitClient(t, clientAddr, serverAddr, []byte("danchaofan"), []byte("19501125"), clientReq, nil); err != nil {
			t.Errorf("[%s] maoanying failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
		wg.Done()
	}()
	<-clientReq.Ready

	// Wait for server to accept the first client request.
	for i := 0; i < 50; i++ {
		if len(server.sessions) == 1 {
			break
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Logf("number of UDP sessions before replay: %d", len(server.sessions))
	if len(server.sessions) != 1 {
		t.Errorf("number of UDP sessions before replay is not 1")
	}

	// Get the current replay counter.
	replayCnt := replay.NewSession.Load()
	t.Logf("replay.NewSession value before replay: %d", replayCnt)

	// Replay the client's request to the server.
	replayConn, err := net.ListenUDP("udp", attackUDPAddr)
	if err != nil {
		t.Fatalf("net.ListenUDP() on %v failed: %v", attackUDPAddr, err)
	}
	defer replayConn.Close()
	replayConn.WriteTo(clientReq.Data, serverUDPAddr)

	// The replay counter should increase.
	increased := false
	for i := 0; i < 50; i++ {
		if replay.NewSession.Load() > replayCnt {
			increased = true
			break
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Logf("replay.NewSession value after replay: %d", replay.NewSession.Load())
	if !increased {
		t.Errorf("replay.NewSession value %d is not changed after replay client request", replay.NewSession.Load())
	}

	// The number of UDP sessions in the server side should not increase.
	t.Logf("number of UDP sessions after replay: %d", len(server.sessions))
	if len(server.sessions) > 1 {
		t.Errorf("number of UDP sessions is changed from %d to %d after replay client request", 1, len(server.sessions))
	}

	close(clientReq.Finish)
	wg.Wait()
	server.Close()
	replayCache.Clear()
	time.Sleep(1 * time.Second) // Wait for resources to be released.
}

// TestReplayServerResponseToServer creates 1 client, 1 server and 1 monitor.
// The monitor records the traffic between the client and server, then replays
// the server's response back to the server. The server should drop the replay
// packet before processing it.
func TestReplayServerResponseToServer(t *testing.T) {
	serverPort, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	clientPort, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	attackPort, err := netutil.UnusedUDPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedUDPPort() failed: %v", err)
	}
	serverAddr := fmt.Sprintf("127.0.0.1:%d", serverPort)
	clientAddr := fmt.Sprintf("127.0.0.1:%d", clientPort)
	attackAddr := fmt.Sprintf("127.0.0.1:%d", attackPort)
	serverUDPAddr, _ := net.ResolveUDPAddr("udp", serverAddr)
	attackUDPAddr, _ := net.ResolveUDPAddr("udp", attackAddr)
	users := map[string]*appctlpb.User{
		"danchaofan": {
			Name:     proto.String("danchaofan"),
			Password: proto.String("19501125"),
		},
	}

	server, err := ListenWithOptions(serverAddr, users)
	if err != nil {
		t.Fatalf("ListenWithOptions() failed: %v", err)
	}

	go func() {
		for {
			s, err := server.Accept()
			if err != nil {
				return
			} else {
				t.Logf("[%s] accepting new connection from %v", time.Now().Format(testtool.TimeLayout), s.RemoteAddr())
				go func() {
					if err = testtool.TestHelperServeConn(s); err != nil {
						return
					}
				}()
			}
		}
	}()
	time.Sleep(1 * time.Second)

	serverResp := &testtool.ReplayRecord{
		Ready:  make(chan struct{}),
		Finish: make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := runCloseWaitClient(t, clientAddr, serverAddr, []byte("danchaofan"), []byte("19501125"), nil, serverResp); err != nil {
			t.Errorf("[%s] maoanying failed: %v", time.Now().Format(testtool.TimeLayout), err)
		}
		wg.Done()
	}()
	<-serverResp.Ready

	// Get the current replay counter.
	replayCnt := replay.NewSession.Load()
	t.Logf("replay.NewSession value before replay: %d", replay.NewSession.Load())

	// Replay the server's response back to the server.
	replayConn, err := net.ListenUDP("udp", attackUDPAddr)
	if err != nil {
		t.Fatalf("net.ListenUDP() on %v failed: %v", attackUDPAddr, err)
	}
	defer replayConn.Close()
	replayConn.WriteTo(serverResp.Data, serverUDPAddr)

	// The replay counter should increase.
	increased := false
	for i := 0; i < 50; i++ {
		if replay.NewSession.Load() > replayCnt {
			increased = true
			break
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Logf("replay.NewSession value after replay: %d", replay.NewSession.Load())
	if !increased {
		t.Errorf("replay.NewSession value %d is not changed after replay client request", replay.NewSession.Load())
	}

	// The number of UDP sessions in the server side should not increase.
	t.Logf("number of UDP sessions after replay: %d", len(server.sessions))
	if len(server.sessions) > 1 {
		t.Errorf("number of UDP sessions is changed from %d to %d after replay client request", 1, len(server.sessions))
	}

	close(serverResp.Finish)
	wg.Wait()
	server.Close()
	replayCache.Clear()
	time.Sleep(1 * time.Second) // Wait for resources to be released.
}
