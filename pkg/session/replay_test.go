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

package session

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
	"github.com/enfein/mieru/pkg/kcp"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/recording"
)

const timeLayout string = "15:04:05.00"

type record struct {
	data   []byte        // the recorded data
	ready  chan struct{} // indicate the data is recorded and ready to process
	finish chan struct{} // indicate the record is processed and no longer needed
}

func runCloseWaitClient(t *testing.T, laddr, raddr string, username, password []byte, clientReq, serverResp *record) error {
	hashedPassword := cipher.HashPassword(password, username)
	block, err := cipher.BlockCipherFromPassword(hashedPassword)
	if err != nil {
		return fmt.Errorf("cipher.BlockCipherFromPassword() failed: %w", err)
	}
	sess, err := DialWithOptions(context.Background(), "udp", laddr, raddr, block)
	if err != nil {
		return fmt.Errorf("DialWithOptions() failed: %w", err)
	}
	defer sess.Close()
	t.Logf("[%s] client is running on %v", time.Now().Format(timeLayout), laddr)

	respBuf := make([]byte, kcp.MaxBufSize)
	data := TestHelperGenRot13Input(1024)

	// Send data to server.
	sess.startRecording()
	if _, err = sess.Write(data); err != nil {
		return fmt.Errorf("Write() failed: %w", err)
	}
	sess.SetReadDeadline(time.Now().Add(30 * time.Second))

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
					clientReq.data = records[j].Data()
					found = true
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !found {
			return fmt.Errorf("egress recording is not found")
		}
		close(clientReq.ready)
	}

	// Get and verify server response.
	size, err := sess.Read(respBuf)
	if err != nil {
		return fmt.Errorf("Read() failed: %w", err)
	}
	revert, err := TestHelperRot13(respBuf[:size])
	if err != nil {
		return fmt.Errorf("TestHelperRot13() failed: %w", err)
	}
	if !bytes.Equal(data, revert) {
		return fmt.Errorf("verification failed")
	}

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
					serverResp.data = records[j].Data()
					found = true
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !found {
			return fmt.Errorf("ingress recording is not found")
		}
		close(serverResp.ready)
		<-serverResp.finish
	}
	return nil
}

// TestReplayServerResponse creates 1 client, 1 server and 1 monitor.
// The monitor records the traffic between the client and server, then replays
// the server's response back to the client. The client should drop the replay
// packet before processing it.
func TestReplayServerResponse(t *testing.T) {
	kcp.TestOnlySegmentDropRate = ""
	serverAddr := "127.0.0.1:12320"
	clientAddr := "127.0.0.1:12321"
	attackAddr := "127.0.0.1:12322"
	clientUDPAddr, _ := net.ResolveUDPAddr("udp", clientAddr)
	attackUDPAddr, _ := net.ResolveUDPAddr("udp", attackAddr)
	users := map[string]*appctlpb.User{
		"danchaofan": {
			Name:     "danchaofan",
			Password: "19501125",
		},
	}

	server, err := ListenWithOptions(serverAddr, users)
	if err != nil {
		t.Fatalf("ListenWithOptions() failed: %v", err)
	}

	go func() {
		for {
			s, err := server.AcceptKCP()
			if err != nil {
				return
			} else {
				t.Logf("[%s] accepting new connection from %v", time.Now().Format(timeLayout), s.RemoteAddr())
				go func() {
					if err = TestHelperServeConn(s); err != nil {
						return
					}
				}()
			}
		}
	}()
	time.Sleep(1 * time.Second)

	serverResp := &record{
		ready:  make(chan struct{}),
		finish: make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := runCloseWaitClient(t, clientAddr, serverAddr, []byte("danchaofan"), []byte("19501125"), nil, serverResp); err != nil {
			t.Errorf("[%s] maoanying failed: %v", time.Now().Format(timeLayout), err)
		}
		wg.Done()
	}()
	<-serverResp.ready

	// Get the current UDP error counter.
	errCnt := metrics.UDPInErrors
	t.Logf("metrics.UDPInErrors value before replay: %d", metrics.UDPInErrors)

	// Replay the server's response to the client.
	replayConn, err := net.ListenUDP("udp", attackUDPAddr)
	if err != nil {
		t.Fatalf("net.ListenUDP() on %v failed: %v", attackUDPAddr, err)
	}
	defer replayConn.Close()
	replayConn.WriteTo(serverResp.data, clientUDPAddr)

	// The UDP error counter should increase.
	increased := false
	for i := 0; i < 300; i++ {
		if metrics.UDPInErrors > errCnt {
			increased = true
			break
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Logf("metrics.UDPInErrors value after replay: %d", metrics.UDPInErrors)
	if !increased {
		t.Errorf("metrics.UDPInErrors value %d is not changed after replay server response", metrics.UDPInErrors)
	}

	close(serverResp.finish)
	wg.Wait()
	server.Close()
	time.Sleep(100 * time.Millisecond) // Wait for resources to be released.
}

// TestReplayServerResponse creates 1 client, 1 server and 1 monitor.
// The monitor records the traffic between the client and server, then replays
// the client's request to the server. The monitor should not get any response
// from the server.
func TestReplayClientRequest(t *testing.T) {
	kcp.TestOnlySegmentDropRate = ""
	serverAddr := "127.0.0.1:12325"
	clientAddr := "127.0.0.1:12326"
	attackAddr := "127.0.0.1:12327"
	serverUDPAddr, _ := net.ResolveUDPAddr("udp", serverAddr)
	attackUDPAddr, _ := net.ResolveUDPAddr("udp", attackAddr)
	users := map[string]*appctlpb.User{
		"danchaofan": {
			Name:     "danchaofan",
			Password: "19501125",
		},
	}

	server, err := ListenWithOptions(serverAddr, users)
	if err != nil {
		t.Fatalf("ListenWithOptions() failed: %v", err)
	}

	go func() {
		for {
			s, err := server.AcceptKCP()
			if err != nil {
				return
			} else {
				t.Logf("[%s] accepting new connection from %v", time.Now().Format(timeLayout), s.RemoteAddr())
				go func() {
					if err = TestHelperServeConn(s); err != nil {
						return
					}
				}()
			}
		}
	}()
	time.Sleep(1 * time.Second)

	clientReq := &record{
		ready:  make(chan struct{}),
		finish: make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := runCloseWaitClient(t, clientAddr, serverAddr, []byte("danchaofan"), []byte("19501125"), clientReq, nil); err != nil {
			t.Errorf("[%s] maoanying failed: %v", time.Now().Format(timeLayout), err)
		}
		wg.Done()
	}()
	<-clientReq.ready

	// Wait for server to accept the first client request.
	for i := 0; i < 300; i++ {
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
	replayCnt := metrics.ReplayNewSession
	t.Logf("metrics.ReplayNewSession value before replay: %d", metrics.ReplayNewSession)

	// Replay the client's request to the server.
	replayConn, err := net.ListenUDP("udp", attackUDPAddr)
	if err != nil {
		t.Fatalf("net.ListenUDP() on %v failed: %v", attackUDPAddr, err)
	}
	defer replayConn.Close()
	replayConn.WriteTo(clientReq.data, serverUDPAddr)

	// The replay counter should increase.
	increased := false
	for i := 0; i < 300; i++ {
		if metrics.ReplayNewSession > replayCnt {
			increased = true
			break
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Logf("metrics.ReplayNewSession value after replay: %d", metrics.ReplayNewSession)
	if !increased {
		t.Errorf("metrics.ReplayNewSession value %d is not changed after replay client request", metrics.ReplayNewSession)
	}

	// The number of UDP sessions in the server side should not increase.
	t.Logf("number of UDP sessions after replay: %d", len(server.sessions))
	if len(server.sessions) > 1 {
		t.Errorf("number of UDP sessions is changed from %d to %d after replay client request", 1, len(server.sessions))
	}

	close(clientReq.finish)
	wg.Wait()
	server.Close()
	time.Sleep(100 * time.Millisecond) // Wait for resources to be released.
}
