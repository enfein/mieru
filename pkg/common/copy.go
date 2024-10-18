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

package common

import (
	"io"
)

// BidiCopy does bi-directional data copy.
func BidiCopy(conn1, conn2 io.ReadWriteCloser) error {
	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn1, conn2)
		conn1.Close()
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(conn2, conn1)
		conn2.Close()
		errCh <- err
	}()

	err := <-errCh
	<-errCh
	return err
}
