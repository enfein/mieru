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

package recording_test

import (
	"bytes"
	"testing"

	"github.com/enfein/mieru/pkg/recording"
)

func TestRecords(t *testing.T) {
	records := recording.NewRecords()
	data := []byte{0x01, 0x02}
	records.Append(data, recording.Egress)

	if records.Size() != 1 {
		t.Errorf("records.Size() = %d, want %d", records.Size(), 1)
	}
	all := records.Export()
	if !bytes.Equal(all[0].Data(), data) {
		t.Errorf("recorded data do not equal to original data")
	}
	if all[0].Direction() != recording.Egress {
		t.Errorf("Direction() = %v, want %v", all[0].Direction(), recording.Egress)
	}

	records.Clear()
	if records.Size() != 0 {
		t.Errorf("records.Size() = %d, want %d", records.Size(), 0)
	}
}
