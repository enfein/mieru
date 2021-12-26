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

package session_test

import (
	"bytes"
	"context"
	"fmt"
	mrand "math/rand"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/kcp"
	"github.com/enfein/mieru/pkg/session"
)

const (
	serverAddr string = "127.0.0.1:12315"
	timeLayout string = "15:04:05.00"
)

func runClient(t *testing.T, laddr string, username, password []byte) error {
	hashedPassword := cipher.HashPassword(password, username)
	block, err := cipher.BlockCipherFromPassword(hashedPassword)
	if err != nil {
		return fmt.Errorf("cipher.BlockCipherFromPassword() failed: %w", err)
	}
	sess, err := session.DialWithOptions(context.Background(), "udp", laddr, serverAddr, block)
	if err != nil {
		return fmt.Errorf("session.DialWithOptions() failed: %w", err)
	}
	defer sess.Close()
	t.Logf("[%s] client is running on %v", time.Now().Format(timeLayout), laddr)

	respBuf := make([]byte, kcp.MaxBufSize)
	for i := 0; i < 100; i++ {
		sleepMillis := 100 + mrand.Intn(100)
		time.Sleep(time.Duration(sleepMillis) * time.Millisecond)

		data := session.TestHelperGenRot13Input(1024)

		// Send data to server.
		if _, err = sess.Write(data); err != nil {
			return fmt.Errorf("Write() failed: %w", err)
		}
		sess.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Get and verify server response.
		size, err := sess.Read(respBuf)
		if err != nil {
			return fmt.Errorf("Read() failed: %w", err)
		}
		revert, err := session.TestHelperRot13(respBuf[:size])
		if err != nil {
			return fmt.Errorf("session.TestHelperRot13() failed: %w", err)
		}
		if !bytes.Equal(data, revert) {
			return fmt.Errorf("verification failed")
		}
	}
	return nil
}

// TestKCPSessions creates one listener and four clients. Each client sends
// some data (in format [A-Za-z]+) to the listener. The listener returns the
// ROT13 (rotate by 13 places) of the data back to the client.
func TestKCPSessions(t *testing.T) {
	kcp.TestOnlySegmentDropRate = "5"
	users := map[string]*appctlpb.User{
		"dengxiaoping": {
			Name:     "dengxiaoping",
			Password: "19890604",
		},
		"jiangzemin": {
			Name:     "jiangzemin",
			Password: "20001027",
		},
		"hujintao": {
			Name:     "hujintao",
			Password: "20080512",
		},
		"xijinping": {
			Name:     "xijinping",
			Password: "20200630",
		},
	}
	party, err := session.ListenWithOptions(serverAddr, users)
	if err != nil {
		t.Fatalf("session.ListenWithOptions() failed: %v", err)
	}

	go func() {
		for {
			s, err := party.AcceptKCP()
			if err != nil {
				return
			} else {
				t.Logf("[%s] accepting new connection from %v", time.Now().Format(timeLayout), s.RemoteAddr())
				go func() {
					if err = session.TestHelperServeConn(s); err != nil {
						return
					}
				}()
			}
		}
	}()
	time.Sleep(1 * time.Second)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		if err := runClient(t, "127.0.0.1:12316", []byte("jiangzemin"), []byte("20001027")); err != nil {
			t.Errorf("[%s] jiangzemin failed: %v", time.Now().Format(timeLayout), err)
		}
		wg.Done()
	}()
	go func() {
		if err := runClient(t, "127.0.0.1:12317", []byte("xijinping"), []byte("20200630")); err != nil {
			t.Errorf("[%s] xijinping failed: %v", time.Now().Format(timeLayout), err)
		}
		wg.Done()
	}()
	wg.Wait()

	party.Close()
	time.Sleep(100 * time.Millisecond) // Wait for resources to be released.
}
