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
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/enfein/mieru/v3/pkg/log"
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

// GetAll returns all metrics from the metric group.
func (g *MetricGroup) GetAll() []Metric {
	var l []Metric
	g.metrics.Range(func(_, v any) bool {
		l = append(l, v.(Metric))
		return true
	})
	return l
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

var _ json.Marshaler = MetricGroupList{}

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

// MarshalJSON export the content of MetricGroupList in JSON format.
func (l MetricGroupList) MarshalJSON() ([]byte, error) {
	sort.Sort(l)

	marshalGroup := func(sb *strings.Builder, g *MetricGroup) {
		metrics := make(map[string]int64)
		names := make([]string, 0)
		g.metrics.Range(func(k, v any) bool {
			m := v.(Metric)
			metrics[m.Name()] = m.Load()
			names = append(names, m.Name())
			return true
		})
		sort.Strings(names)
		for j := 0; j < len(names); j++ {
			name := names[j]
			fmt.Fprintf(sb, `"%s": %d`, name, metrics[name])
			if j != len(names)-1 {
				sb.WriteString(",")
			}
		}
	}

	processingUserMetrics := false
	var sb strings.Builder
	sb.WriteString("{") // begin of metric group list
	for i := 0; i < l.Len(); i++ {
		g := l[i]
		if !strings.HasPrefix(g.name, userMetricGroupPrefix) {
			// non user metrics
			fmt.Fprintf(&sb, `"%s": {`, g.name)
			marshalGroup(&sb, g)
			sb.WriteString("}")
			if i != l.Len()-1 {
				sb.WriteString(",")
			}
		} else {
			// user metrics
			if !processingUserMetrics {
				processingUserMetrics = true
				sb.WriteString(`"users": {`) // begin of user metrics section
			}
			userName := strings.TrimPrefix(g.name, userMetricGroupPrefix)
			fmt.Fprintf(&sb, `"%s": {`, userName)
			marshalGroup(&sb, g)
			sb.WriteString("}")
			if i != l.Len()-1 {
				nextGroup := l[i+1]
				if strings.HasPrefix(nextGroup.name, userMetricGroupPrefix) {
					sb.WriteString(",")
				} else {
					processingUserMetrics = false
					sb.WriteString("},") // end of user metrics section
				}
			} else {
				processingUserMetrics = false
				sb.WriteString("}") // end of user metrics section
			}
		}
	}
	sb.WriteString("}") // end of metric group list

	// Make JSON pretty.
	raw := []byte(sb.String())
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", "    "); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
