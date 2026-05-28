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
	"net"
	"reflect"
	"testing"

	"github.com/enfein/mieru/v3/apis/constant"
)

func TestSocks5RequestHelpers(t *testing.T) {
	input := []byte{constant.Socks5Version, constant.Socks5UDPAssociateCmd, 0, constant.Socks5FQDNAddress, 11, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm', 0x1f, 0x90}

	req, err := ReadSocks5Request(bytes.NewBuffer(input))
	if err != nil {
		t.Fatalf("ReadSocks5Request() failed: %v", err)
	}
	if req.Command != constant.Socks5UDPAssociateCmd {
		t.Errorf("got command %v, want %v", req.Command, constant.Socks5UDPAssociateCmd)
	}
	if req.DstAddr.FQDN != "example.com" || req.DstAddr.Port != 8080 {
		t.Errorf("got DstAddr %+v, want example.com:8080", req.DstAddr)
	}
	if !bytes.Equal(req.Raw, input) {
		t.Errorf("got raw %v, want %v", req.Raw, input)
	}

	var output bytes.Buffer
	if err := WriteSocks5Request(&output, req.Command, req.DstAddr); err != nil {
		t.Fatalf("WriteSocks5Request() failed: %v", err)
	}
	if !bytes.Equal(output.Bytes(), input) {
		t.Errorf("got output %v, want %v", output.Bytes(), input)
	}
}

func TestRequestReadWrite(t *testing.T) {
	testCases := []struct {
		input   []byte
		request *Request
	}{
		{
			input: []byte{constant.Socks5Version, constant.Socks5ConnectCmd, 0, constant.Socks5IPv4Address, 127, 0, 0, 1, 0, 80},
			request: &Request{
				Command: constant.Socks5ConnectCmd,
				DstAddr: AddrSpec{
					IP:   net.IP{127, 0, 0, 1},
					Port: 80,
				},
			},
		},
		{
			input: []byte{constant.Socks5Version, constant.Socks5ConnectCmd, 0, constant.Socks5IPv6Address, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 80},
			request: &Request{
				Command: constant.Socks5ConnectCmd,
				DstAddr: AddrSpec{
					IP:   net.ParseIP("::1"),
					Port: 80,
				},
			},
		},
		{
			input: []byte{constant.Socks5Version, constant.Socks5ConnectCmd, 0, constant.Socks5FQDNAddress, 9, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't', 0, 80},
			request: &Request{
				Command: constant.Socks5ConnectCmd,
				DstAddr: AddrSpec{
					FQDN: "localhost",
					Port: 80,
				},
			},
		},
	}

	for _, tc := range testCases {
		req := &Request{}
		err := req.ReadFromSocks5(bytes.NewBuffer(tc.input))
		if err != nil {
			t.Fatalf("ReadFromSocks5() failed: %v", err)
		}
		if req.Command != tc.request.Command {
			t.Errorf("got command %v, want %v", req.Command, tc.request.Command)
		}
		if !reflect.DeepEqual(req.DstAddr, tc.request.DstAddr) {
			t.Errorf("got DstAddr %+v, want %+v", req.DstAddr, tc.request.DstAddr)
		}

		var output bytes.Buffer
		err = req.WriteToSocks5(&output)
		if err != nil {
			t.Fatalf("WriteToSocks5() failed: %v", err)
		}
		outputBytes := output.Bytes()
		if !bytes.Equal(outputBytes, tc.input) {
			t.Errorf("got %v, want %v", outputBytes, tc.input)
		}
	}
}

func TestRequestToNetAddrSpec(t *testing.T) {
	addr := AddrSpec{
		FQDN: "example.com",
		Port: 80,
	}
	testCases := []struct {
		name    string
		req     Request
		want    NetAddrSpec
		wantErr bool
	}{
		{
			name: "connect command",
			req: Request{
				Command: constant.Socks5ConnectCmd,
				DstAddr: addr,
			},
			want: NetAddrSpec{
				AddrSpec: addr,
				Net:      "tcp",
			},
			wantErr: false,
		},
		{
			name: "udp associate command",
			req: Request{
				Command: constant.Socks5UDPAssociateCmd,
				DstAddr: addr,
			},
			want: NetAddrSpec{
				AddrSpec: addr,
				Net:      "udp",
			},
			wantErr: false,
		},
		{
			name: "unsupported command",
			req: Request{
				Command: constant.Socks5BindCmd,
				DstAddr: addr,
			},
			want:    NetAddrSpec{},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.req.ToNetAddrSpec()
			if (err != nil) != tc.wantErr {
				t.Errorf("ToNetAddrSpec() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ToNetAddrSpec() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSocks5ResponseHelpers(t *testing.T) {
	input := []byte{constant.Socks5Version, constant.Socks5ReplySuccess, 0, constant.Socks5IPv4Address, 127, 0, 0, 1, 0x23, 0x29}

	resp, err := ReadSocks5Response(bytes.NewBuffer(input))
	if err != nil {
		t.Fatalf("ReadSocks5Response() failed: %v", err)
	}
	if resp.Reply != constant.Socks5ReplySuccess {
		t.Errorf("got reply %v, want %v", resp.Reply, constant.Socks5ReplySuccess)
	}
	if !reflect.DeepEqual(resp.BindAddr, AddrSpec{IP: net.IP{127, 0, 0, 1}, Port: 9001}) {
		t.Errorf("got BindAddr %+v, want 127.0.0.1:9001", resp.BindAddr)
	}
	if !bytes.Equal(resp.Raw, input) {
		t.Errorf("got raw %v, want %v", resp.Raw, input)
	}

	var output bytes.Buffer
	if err := WriteSocks5Response(&output, resp.Reply, resp.BindAddr); err != nil {
		t.Fatalf("WriteSocks5Response() failed: %v", err)
	}
	if !bytes.Equal(output.Bytes(), input) {
		t.Errorf("got output %v, want %v", output.Bytes(), input)
	}
}

func TestResponseReadWrite(t *testing.T) {
	testCases := []struct {
		input    []byte
		response *Response
	}{
		{
			input: []byte{constant.Socks5Version, constant.Socks5ReplySuccess, 0, constant.Socks5IPv4Address, 127, 0, 0, 1, 0, 80},
			response: &Response{
				Reply: constant.Socks5ReplySuccess,
				BindAddr: AddrSpec{
					IP:   net.IP{127, 0, 0, 1},
					Port: 80,
				},
			},
		},
		{
			input: []byte{constant.Socks5Version, constant.Socks5ReplySuccess, 0, constant.Socks5IPv6Address, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 80},
			response: &Response{
				Reply: constant.Socks5ReplySuccess,
				BindAddr: AddrSpec{
					IP:   net.ParseIP("::1"),
					Port: 80,
				},
			},
		},
		{
			input: []byte{constant.Socks5Version, constant.Socks5ReplySuccess, 0, constant.Socks5FQDNAddress, 9, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't', 0, 80},
			response: &Response{
				Reply: constant.Socks5ReplySuccess,
				BindAddr: AddrSpec{
					FQDN: "localhost",
					Port: 80,
				},
			},
		},
	}

	for _, tc := range testCases {
		resp := &Response{}
		err := resp.ReadFromSocks5(bytes.NewBuffer(tc.input))
		if err != nil {
			t.Fatalf("ReadFromSocks5() failed: %v", err)
		}
		if resp.Reply != tc.response.Reply {
			t.Errorf("got reply %v, want %v", resp.Reply, tc.response.Reply)
		}
		if !reflect.DeepEqual(resp.BindAddr, tc.response.BindAddr) {
			t.Errorf("got BindAddr %+v, want %+v", resp.BindAddr, tc.response.BindAddr)
		}

		var output bytes.Buffer
		err = resp.WriteToSocks5(&output)
		if err != nil {
			t.Fatalf("WriteToSocks5() failed: %v", err)
		}
		outputBytes := output.Bytes()
		if !bytes.Equal(outputBytes, tc.input) {
			t.Errorf("got %v, want %v", outputBytes, tc.input)
		}
	}
}
