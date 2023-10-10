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

package metrics

import "testing"

func TestGauge(t *testing.T) {
	g := &Gauge{name: "gauge"}

	if g.Name() != "gauge" {
		t.Errorf("Name() = %v, want %v", g.Name(), "gauge")
	}
	if g.Type() != GAUGE {
		t.Errorf("Type() = %v, want %v", g.Type(), GAUGE)
	}

	g.Store(10)
	g.Add(-5)
	val := g.Load()
	if val != 5 {
		t.Errorf("Load() = %v, want %v", val, 5)
	}
}
