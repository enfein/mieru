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
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/kcp"
	"github.com/enfein/mieru/pkg/session"
)

func rot13(in []byte) ([]byte, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("input is empty")
	}
	match, err := regexp.MatchString("[A-Za-z]+", string(in))
	if err != nil {
		return nil, fmt.Errorf("regexp.MatchString() failed: %v", err)
	}
	if !match {
		return nil, fmt.Errorf("input format is invalid")
	}
	out := make([]byte, len(in))
	for i := 0; i < len(in); i++ {
		if (in[i] >= 65 && in[i] <= 77) || (in[i] >= 97 && in[i] <= 109) {
			out[i] = in[i] + 13
		} else if (in[i] >= 78 && in[i] <= 90) || (in[i] >= 110 && in[i] <= 122) {
			out[i] = in[i] - 13
		}
	}
	return out, nil
}

func serveConn(t *testing.T, conn *session.UDPSession) error {
	defer conn.Close()
	buf := make([]byte, kcp.MaxBufSize)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return fmt.Errorf("Read() failed: %v", err)
		}
		if n == 0 {
			continue
		}
		out, err := rot13(buf[:n])
		if err != nil {
			return fmt.Errorf("rot13() failed: %v", err)
		}
		if _, err = conn.Write(out); err != nil {
			return fmt.Errorf("Write() failed: %v", err)
		}
	}
}

func runClient(t *testing.T, laddr string, username, password []byte) error {
	hashedPassword := cipher.HashPassword(password, username)
	block, err := cipher.BlockCipherFromPassword(hashedPassword)
	if err != nil {
		return fmt.Errorf("cipher.BlockCipherFromPassword() failed: %v", err)
	}
	sess, err := session.DialWithOptions(context.Background(), "udp", laddr, "127.0.0.1:12315", block)
	if err != nil {
		return fmt.Errorf("session.DialWithOptions() failed: %v", err)
	}
	defer sess.Close()

	data := make([]byte, 1024)
	respBuf := make([]byte, kcp.MaxBufSize)
	mrand.Seed(time.Now().UnixNano())

	for i := 0; i < 50; i++ {
		sleepMillis := 500 + mrand.Intn(500)
		time.Sleep(time.Duration(sleepMillis) * time.Millisecond)

		// Generate random data.
		for j := 0; j < 1024; j++ {
			p := mrand.Float32()
			if p <= 0.5 {
				data[j] = byte(mrand.Int31n(26) + 65)
			} else {
				data[j] = byte(mrand.Int31n(26) + 97)
			}
		}

		// Send data to server.
		if _, err = sess.Write(data); err != nil {
			return fmt.Errorf("Write() failed: %v", err)
		}

		// Get and verify server response.
		size, err := sess.Read(respBuf)
		if err != nil {
			return fmt.Errorf("Read() failed: %v", err)
		}
		revert, err := rot13(respBuf[:size])
		if err != nil {
			return fmt.Errorf("rot13() failed: %v", err)
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
	party, err := session.ListenWithOptions("127.0.0.1:12315", users)
	if err != nil {
		t.Fatalf("session.ListenWithOptions() failed: %v", err)
	}

	go func() {
		for {
			s, err := party.AcceptKCP()
			if err != nil {
				t.Errorf("AcceptKCP() failed: %v", err)
			} else {
				t.Logf("accepting new connection from %v", s.RemoteAddr())
				go func() {
					if err = serveConn(t, s); err != nil {
						t.Errorf("serveConn() failed: %v", err)
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
			t.Errorf("jiangzemin failed: %v", err)
		}
		wg.Done()
	}()
	go func() {
		if err := runClient(t, "127.0.0.1:12317", []byte("xijinping"), []byte("20200630")); err != nil {
			t.Errorf("xijinping failed: %v", err)
		}
		wg.Done()
	}()
	wg.Wait()
}
