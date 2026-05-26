// Copyright (C) 2026  mieru authors
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

package socks5

import (
	"bytes"
	"errors"
	"net"
	"testing"

	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

func TestSocks5UDPDatagramAllowsZeroLengthPayload(t *testing.T) {
	dst := model.AddrSpec{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 53,
	}
	pkt, err := newSocks5UDPDatagram(dst, nil)
	if err != nil {
		t.Fatalf("newSocks5UDPDatagram() failed: %v", err)
	}

	datagram, err := parseSocks5UDPDatagram(pkt)
	if err != nil {
		t.Fatalf("parseSocks5UDPDatagram() failed: %v", err)
	}
	if !bytes.Equal(datagram.Header, pkt) {
		t.Fatalf("datagram header = %v, want %v", datagram.Header, pkt)
	}
	if len(datagram.Payload) != 0 {
		t.Fatalf("datagram payload length = %d, want 0", len(datagram.Payload))
	}

	payload, err := unwrapSocks5UDPPacket(pkt)
	if err != nil {
		t.Fatalf("unwrapSocks5UDPPacket() failed: %v", err)
	}
	if len(payload) != 0 {
		t.Fatalf("unwrapped payload length = %d, want 0", len(payload))
	}

	addr, payload, err := parseUDPAssociateDatagram(pkt, nil)
	if err != nil {
		t.Fatalf("parseUDPAssociateDatagram() failed: %v", err)
	}
	if !addr.IP.Equal(dst.IP) || addr.Port != dst.Port {
		t.Fatalf("destination = %v, want %v:%d", addr, dst.IP, dst.Port)
	}
	if len(payload) != 0 {
		t.Fatalf("parsed payload length = %d, want 0", len(payload))
	}
}

func TestSocks5UDPDatagramRejectsTruncatedHeader(t *testing.T) {
	pkt := []byte{
		0,
		0,
		0,
		constant.Socks5IPv4Address,
		127,
		0,
		0,
		1,
		0,
	}

	_, err := parseSocks5UDPDatagram(pkt)
	if !errors.Is(err, stderror.ErrNoEnoughData) {
		t.Fatalf("parseSocks5UDPDatagram() error = %v, want %v", err, stderror.ErrNoEnoughData)
	}
}
