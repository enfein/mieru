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
	"sync"
)

// metricMap is a global map that holds all the registered metrics.
var metricMap sync.Map

// RegisterMetric registers a new metric.
// The caller should not take the ownership of the returned pointer.
// If the same metric is registered multiple times, the pointer to the first object is returned.
func RegisterMetric(groupName, metricName string, metricType MetricType) Metric {
	group, _ := metricMap.LoadOrStore(groupName, &MetricGroup{
		name: groupName,
	})
	metricGroup := group.(*MetricGroup)
	metricGroup.EnableLogging()
	var m Metric
	switch metricType {
	case COUNTER:
		m = &Counter{name: metricName}
	case COUNTER_TIME_SERIES:
		m = &Counter{name: metricName, timeSeries: true}
	case GAUGE:
		m = &Gauge{name: metricName}
	default:
		panic(fmt.Sprintf("unrecognized metric type %v", metricType))
	}
	metric, _ := metricGroup.metrics.LoadOrStore(metricName, m)
	return metric.(Metric)
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
