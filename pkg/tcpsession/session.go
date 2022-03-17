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
	"context"
	"net"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/recording"
)

type TCPSession struct {
	net.Conn

	block cipher.BlockCipher

	recordingEnabled bool
	recordedPackets  recording.Records
}

func (s *TCPSession) Read(b []byte) (n int, err error) {
	return 0, nil
}

func (s *TCPSession) Write(b []byte) (n int, err error) {
	return 0, nil
}

func DialWithOptions(ctx context.Context, network, laddr, raddr string, block cipher.BlockCipher) (*TCPSession, error) {
	return nil, nil
}

func DialWithOptionsReturnConn(ctx context.Context, network, laddr, raddr string, block cipher.BlockCipher) (net.Conn, error) {
	return DialWithOptions(ctx, network, laddr, raddr, block)
}
