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

package model

import (
	"bytes"
	"fmt"
	"io"

	"github.com/enfein/mieru/v3/apis/constant"
)

type Request struct {
	// Requested command.
	Command uint8
	// Desired destination.
	DstAddr AddrSpec
	// Raw request bytes.
	Raw []byte
}

func (r Request) String() string {
	return fmt.Sprintf("Request{command=%d, destination=%v}", r.Command, r.DstAddr)
}

// ReadFromSocks5 reads a socks5 request.
func (r *Request) ReadFromSocks5(reader io.Reader) error {
	var buf bytes.Buffer
	tee := io.TeeReader(reader, &buf)

	header := make([]byte, 3)
	if _, err := io.ReadFull(tee, header); err != nil {
		return err
	}
	version := header[0]
	r.Command = header[1]
	if version != constant.Socks5Version {
		return fmt.Errorf("invalid version: %d", version)
	}

	if err := r.DstAddr.ReadFromSocks5(tee); err != nil {
		return err
	}

	r.Raw = buf.Bytes()
	return nil
}

// WriteToSocks5 writes a socks5 request.
func (r *Request) WriteToSocks5(writer io.Writer) error {
	var buf bytes.Buffer

	buf.Write([]byte{constant.Socks5Version, r.Command, 0})
	if err := r.DstAddr.WriteToSocks5(&buf); err != nil {
		return err
	}

	r.Raw = buf.Bytes()
	_, err := writer.Write(r.Raw)
	return err
}

// ToNetAddrSpec converts a socks5 request to a NetAddrSpec object.
func (r Request) ToNetAddrSpec() (NetAddrSpec, error) {
	switch r.Command {
	case constant.Socks5ConnectCmd:
		return NetAddrSpec{
			AddrSpec: r.DstAddr,
			Net:      "tcp",
		}, nil
	case constant.Socks5UDPAssociateCmd:
		return NetAddrSpec{
			AddrSpec: r.DstAddr,
			Net:      "udp",
		}, nil
	default:
		return NetAddrSpec{}, fmt.Errorf("unsupported socks5 command: %d", r.Command)
	}
}

type Response struct {
	// Reply code.
	Reply uint8
	// Server bound address.
	BindAddr AddrSpec
	// Raw response bytes.
	Raw []byte
}

func (r Response) String() string {
	return fmt.Sprintf("Response{reply=%d, bind=%v}", r.Reply, r.BindAddr)
}

// ReadFromSocks5 reads a socks5 response.
func (r *Response) ReadFromSocks5(reader io.Reader) error {
	var buf bytes.Buffer
	tee := io.TeeReader(reader, &buf)

	header := make([]byte, 3)
	if _, err := io.ReadFull(tee, header); err != nil {
		return err
	}
	version := header[0]
	r.Reply = header[1]
	if version != constant.Socks5Version {
		return fmt.Errorf("invalid version: %d", version)
	}

	if err := r.BindAddr.ReadFromSocks5(tee); err != nil {
		return err
	}

	r.Raw = buf.Bytes()
	return nil
}

// WriteToSocks5 writes a socks5 response.
func (r *Response) WriteToSocks5(writer io.Writer) error {
	var buf bytes.Buffer

	buf.Write([]byte{constant.Socks5Version, r.Reply, 0})
	if err := r.BindAddr.WriteToSocks5(&buf); err != nil {
		return err
	}

	r.Raw = buf.Bytes()
	_, err := writer.Write(r.Raw)
	return err
}
