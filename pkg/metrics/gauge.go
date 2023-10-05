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

import "sync/atomic"

// Gauge holds a named int64 value.
type Gauge struct {
	name  string
	value atomic.Int64
}

func (g *Gauge) Name() string {
	return g.name
}

func (g *Gauge) Type() MetricType {
	return GAUGE
}

func (g *Gauge) Add(delta int64) int64 {
	return g.value.Add(delta)
}

func (g *Gauge) Load() int64 {
	return g.value.Load()
}

func (g *Gauge) Store(val int64) {
	g.value.Store(val)
}
