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
	"sync"
)

// metricMap is a global map that holds all the registered metrics.
var metricMap sync.Map

// RegisterMetric registers a new metric.
// The caller should not take the ownership of the returned pointer.
// If the same metric is registered multiple times, the pointer to the first object is returned.
func RegisterMetric(groupName, metricName string) Metric {
	group, _ := metricMap.LoadOrStore(groupName, &MetricGroup{
		name: groupName,
	})
	metricGroup := group.(*MetricGroup)
	metricGroup.EnableLogging()
	metric, _ := metricGroup.metrics.LoadOrStore(metricName, &Gauge{name: metricName})
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
