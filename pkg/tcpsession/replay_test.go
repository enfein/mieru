// Copyright (C) 2022  mieru authors
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

package tcpsession

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/netutil"
	"github.com/enfein/mieru/pkg/recording"
	"github.com/enfein/mieru/pkg/replay"
	"github.com/enfein/mieru/pkg/testtool"
	"google.golang.org/protobuf/proto"
)

func runCloseWaitClient(t *testing.T, laddr, raddr string, username, password []byte, clientReq, serverResp *testtool.ReplayRecord) error {
	hashedPassword := cipher.HashPassword(password, username)
	block, err := cipher.BlockCipherFromPassword(hashedPassword, false)
	if err != nil {
		return fmt.Errorf("cipher.BlockCipherFromPassword() failed: %w", err)
	}
	sess, err := DialWithOptions(context.Background(), "tcp", laddr, raddr, block)
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
			for j := 0; j < len(records); j++ {
				if records[j].Direction() == recording.Egress {
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
			for j := 0; j < len(records); j++ {
				if records[j].Direction() == recording.Ingress {
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

// TestReplayClientRequestToServer creates 1 client, 1 server and 1 monitor.
// The monitor records the traffic between the client and server, then replays
// the client's request to the server. The monitor should not get any response
// from the server.
func TestReplayClientRequestToServer(t *testing.T) {
	serverPort, err := netutil.UnusedTCPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedTCPPort() failed: %v", err)
	}
	clientPort, err := netutil.UnusedTCPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedTCPPort() failed: %v", err)
	}
	attackPort, err := netutil.UnusedTCPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedTCPPort() failed: %v", err)
	}
	serverAddr := fmt.Sprintf("127.0.0.1:%d", serverPort)
	clientAddr := fmt.Sprintf("127.0.0.1:%d", clientPort)
	attackAddr := fmt.Sprintf("127.0.0.1:%d", attackPort)
	serverTCPAddr, _ := net.ResolveTCPAddr("tcp", serverAddr)
	attackTCPAddr, _ := net.ResolveTCPAddr("tcp", attackAddr)
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

	established := metrics.CurrEstablished.Load()
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
		if metrics.CurrEstablished.Load() > established {
			break
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Get the current replay counter.
	replayCnt := replay.NewSession.Load()
	t.Logf("replay.NewSession value before replay: %d", replayCnt)

	// Replay the client's request to the server.
	replayConn, err := net.DialTCP("tcp", attackTCPAddr, serverTCPAddr)
	if err != nil {
		t.Fatalf("net.DialTCP() failed: %v", err)
	}
	defer replayConn.Close()
	replayConn.Write(clientReq.Data)

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

	// The attacker should not receive any data.
	recvBuf := make([]byte, 1500)
	replayConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := replayConn.Read(recvBuf)
	if n != 0 || err != io.EOF {
		t.Errorf("attacker received response from server")
	}

	close(clientReq.Finish)
	wg.Wait()
	server.Close()
	replayCache.Clear()
	time.Sleep(1 * time.Second) // Wait for resources to be released.
}

// TestReplayServerResponseToServer creates 1 client, 1 server and 1 monitor.
// The monitor records the traffic between the client and server, then replays
// the server's response back to the server. The monitor should not get any
// response from the server.
func TestReplayServerResponseToServer(t *testing.T) {
	serverPort, err := netutil.UnusedTCPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedTCPPort() failed: %v", err)
	}
	clientPort, err := netutil.UnusedTCPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedTCPPort() failed: %v", err)
	}
	attackPort, err := netutil.UnusedTCPPort()
	if err != nil {
		t.Fatalf("netutil.UnusedTCPPort() failed: %v", err)
	}
	serverAddr := fmt.Sprintf("127.0.0.1:%d", serverPort)
	clientAddr := fmt.Sprintf("127.0.0.1:%d", clientPort)
	attackAddr := fmt.Sprintf("127.0.0.1:%d", attackPort)
	serverTCPAddr, _ := net.ResolveTCPAddr("tcp", serverAddr)
	attackTCPAddr, _ := net.ResolveTCPAddr("tcp", attackAddr)
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
	t.Logf("replay.NewSession value before replay: %d", replayCnt)

	// Replay the server's response back to the server.
	replayConn, err := net.DialTCP("tcp", attackTCPAddr, serverTCPAddr)
	if err != nil {
		t.Fatalf("net.DialTCP() failed: %v", err)
	}
	defer replayConn.Close()
	replayConn.Write(serverResp.Data)

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

	// The attacker should not receive any data.
	recvBuf := make([]byte, 1500)
	replayConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := replayConn.Read(recvBuf)
	if n != 0 || err != io.EOF {
		t.Errorf("attacker received response from server")
	}

	close(serverResp.Finish)
	wg.Wait()
	server.Close()
	replayCache.Clear()
	time.Sleep(1 * time.Second) // Wait for resources to be released.
}
