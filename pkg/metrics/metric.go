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

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/enfein/mieru/pkg/log"
)

type MetricType uint8

const (
	COUNTER MetricType = iota
	COUNTER_TIME_SERIES
	GAUGE
)

// Metric defines supported operations of a single metric.
type Metric interface {
	// Name returns the name of metric.
	Name() string

	// Type returns the metric type.
	Type() MetricType

	// Add increase or decrease the metric by the given value.
	Add(delta int64) int64

	// Load returns the current value of the metric.
	Load() int64

	// Store sets the metric to a particular value.
	// This operation is only supported by a GAUGE metric.
	Store(val int64)
}

// MetricGroup holds a list of metric under the same group.
type MetricGroup struct {
	name          string
	metrics       sync.Map
	enableLogging atomic.Bool
}

// GetMetric returns one metric from the metric group.
func (g *MetricGroup) GetMetric(name string) (Metric, bool) {
	v, ok := g.metrics.Load(name)
	if !ok {
		return nil, false
	}
	return v.(Metric), true
}

// IsLoggingEnabled returns if logging is enabled in this MetricGroup.
func (g *MetricGroup) IsLoggingEnabled() bool {
	return g.enableLogging.Load()
}

// EnableLogging enables logging of this MetricGroup.
func (g *MetricGroup) EnableLogging() {
	g.enableLogging.Store(true)
}

// DisableLogging disables logging of this MetricGroup.
func (g *MetricGroup) DisableLogging() {
	g.enableLogging.Store(false)
}

// NewLogMsg creates a base log message without fields from the MetricGroup.
func (g *MetricGroup) NewLogMsg() string {
	return fmt.Sprintf("[metrics - %s]", g.name)
}

// NewLogFields creates log fields from the MetricGroup.
func (g *MetricGroup) NewLogFields() log.Fields {
	f := log.Fields{}
	g.metrics.Range(func(k, v any) bool {
		metric := v.(Metric)
		f[metric.Name()] = metric.Load()
		return true
	})
	return f
}

// MetricGroupList is a list of MetricGroup.
type MetricGroupList []*MetricGroup

// Append adds a new MetricGroup to the end of list.
func (l MetricGroupList) Append(group *MetricGroup) MetricGroupList {
	return append(l, group)
}

// Len implements sort.Interface.
func (l MetricGroupList) Len() int {
	return len(l)
}

// Less implements sort.Interface.
func (l MetricGroupList) Less(i, j int) bool {
	// Compare without case.
	return strings.ToLower(l[i].name) < strings.ToLower(l[j].name)
}

// Swap implements sort.Interface.
func (l MetricGroupList) Swap(i, j int) {
	tmp := l[i]
	l[i] = l[j]
	l[j] = tmp
}
