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

package netutil

import "io"

type closeWriter interface {
	CloseWrite() error
}

// BidiCopy does bi-directional data copy.
func BidiCopy(conn1, conn2 io.ReadWriteCloser, isClient bool) error {
	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn1, conn2)
		if isClient {
			// Must call Close() to make sure counter is updated.
			conn1.Close()
		} else {
			// Avoid counter updated twice due to twice Close(), use CloseWrite().
			// The connection still needs to close here to unblock the other side.
			if tcpConn1, ok := conn1.(closeWriter); ok {
				tcpConn1.CloseWrite()
			}
		}
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(conn2, conn1)
		if isClient {
			conn2.Close()
		} else {
			if tcpConn2, ok := conn2.(closeWriter); ok {
				tcpConn2.CloseWrite()
			}
		}
		errCh <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}
	return nil
}
