// Copyright (C) 2024  mieru authors
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

package testtool

import (
	"net"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/enfein/mieru/pkg/util"
)

// NewTestHTTPServer starts a new HTTP server at a random port.
// For each HTTP request, it returns the given response.
func NewTestHTTPServer(resp []byte) *http.Server {
	httpTestPort, err := util.UnusedTCPPort()
	if err != nil {
		panic(err)
	}

	s := &http.Server{
		Addr: ":" + strconv.Itoa(httpTestPort),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Write(resp)
		}),
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	started := make(chan struct{})
	go func() {
		l, err := net.Listen("tcp", s.Addr)
		if err != nil {
			panic(err)
		}
		close(started)
		if err = http.Serve(l, s.Handler); err != nil {
			panic(err)
		}
	}()

	runtime.Gosched()
	<-started
	runtime.Gosched()
	WaitForTCPReady(httpTestPort, 5*time.Second)
	return s
}

// WaitForTCPReady dials to 127.0.0.1:port within the given timeout.
// It panics if dial is not successful within the timeout.
func WaitForTCPReady(port int, timeout time.Duration) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), timeout)
	if err != nil {
		panic(err)
	}
	conn.Close()
}
