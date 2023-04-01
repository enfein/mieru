// Copyright (C) 2023  mieru authors
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

package protocolv2

import (
	mrand "math/rand"
	"reflect"
	"testing"
)

func TestCloseSessionStruct(t *testing.T) {
	s := &sessionStruct{
		BaseStruct: BaseStruct{
			Protocol: closeSessionRequest,
			Epoch:    uint8(mrand.Uint32()),
		},
		RequestID:  mrand.Uint32(),
		SessionID:  mrand.Uint32(),
		StatusCode: uint8(mrand.Uint32()),
		PaddingLen: uint8(mrand.Uint32()),
	}
	b, err := s.Marshal()
	if err != nil {
		t.Fatalf("Marshal() failed: %v", err)
	}
	s2 := &sessionStruct{}
	if err := s2.Unmarshal(b); err != nil {
		t.Fatalf("Unmarshal() failed: %v", err)
	}
	if !reflect.DeepEqual(s, s2) {
		t.Errorf("Not equal:\n%v\n====\n%v", s, s2)
	}
}

func TestDataAckStruct(t *testing.T) {
	s := &dataAckStruct{
		BaseStruct: BaseStruct{
			Protocol: dataServerToClient,
			Epoch:    uint8(mrand.Uint32()),
		},
		SessionID:  mrand.Uint32(),
		Seq:        mrand.Uint32(),
		UnAckSeq:   mrand.Uint32(),
		WindowSize: uint16(mrand.Uint32()),
		Fragment:   uint8(mrand.Uint32()),
		PrefixLen:  uint8(mrand.Uint32()),
		PayloadLen: uint16(mrand.Uint32()),
		SuffixLen:  uint8(mrand.Uint32()),
	}
	b, err := s.Marshal()
	if err != nil {
		t.Fatalf("Marshal() failed: %v", err)
	}
	s2 := &dataAckStruct{}
	if err := s2.Unmarshal(b); err != nil {
		t.Fatalf("Unmarshal() failed: %v", err)
	}
	if !reflect.DeepEqual(s, s2) {
		t.Errorf("Not equal:\n%v\n====\n%v", s, s2)
	}
}
