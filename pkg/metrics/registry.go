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

package metrics

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/enfein/mieru/pkg/log"
)

var metricMap sync.Map

// Metric holds a named int64 value.
// int64 is the only supported metric value type.
type Metric struct {
	name  string
	value atomic.Int64
}

// Name returns the name of metric.
func (m *Metric) Name() string {
	return m.name
}

// Add increase / decrease the metric value.
func (m *Metric) Add(delta int64) int64 {
	return m.value.Add(delta)
}

// Load returns the current metric value.
func (m *Metric) Load() int64 {
	return m.value.Load()
}

// Store saves a new metric value.
func (m *Metric) Store(val int64) {
	m.value.Store(val)
}

// MetricGroup holds a list of metric under the same group.
type MetricGroup struct {
	name          string
	metrics       sync.Map
	enableLogging atomic.Bool
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
		metric := v.(*Metric)
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

// RegisterMetric registers a new metric.
// The caller should not take the ownership of the returned pointer.
// If the same metric is registered multiple times, the pointer to the first object is returned.
func RegisterMetric(groupName, metricName string) *Metric {
	group, _ := metricMap.LoadOrStore(groupName, &MetricGroup{
		name: groupName,
	})
	metricGroup := group.(*MetricGroup)
	metricGroup.EnableLogging()
	metric, _ := metricGroup.metrics.LoadOrStore(metricName, &Metric{name: metricName})
	return metric.(*Metric)
}

// GetMetricGroupByName returns the MetricGroup by name.
// It returns nil if the MetricGroup is not found.
func GetMetricGroupByName(groupName string) *MetricGroup {
	group, ok := metricMap.Load(groupName)
	if !ok {
		return nil
	}
	return group.(*MetricGroup)
}
