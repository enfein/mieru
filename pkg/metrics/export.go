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
	"os"
	"sort"
	"sync"
	"time"

	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/mathext"
	pb "github.com/enfein/mieru/v3/pkg/metrics/metricspb"
	"google.golang.org/protobuf/proto"
)

var logTicker *time.Ticker
var logDuration time.Duration
var metricsDump bool
var metricsDumpFilePath string
var stopLogging chan struct{}
var logMutex sync.Mutex

func init() {
	logDuration = 10 * time.Minute
	stopLogging = make(chan struct{}, 1) // doesn't block
}

// Enable metrics logging with the given time duration.
// This function should not be called again before disable logging.
func EnableLogging() {
	logMutex.Lock()
	defer logMutex.Unlock()
	if logTicker == nil {
		logTicker = time.NewTicker(logDuration)
	} else {
		logTicker.Reset(logDuration)
	}
	go logMetricsLoop()
	log.Infof("enabled metrics logging with duration %v", logDuration)
}

// Disable metrics logging.
func DisableLogging() {
	logMutex.Lock()
	defer logMutex.Unlock()
	if len(stopLogging) == 0 {
		stopLogging <- struct{}{}
	}
	if logTicker != nil {
		logTicker.Stop()
		log.Infof("disabled metrics logging")
	}
}

// Dump metrics to a file when it is logged.
func EnableMetricsDump() error {
	logMutex.Lock()
	defer logMutex.Unlock()
	if metricsDumpFilePath == "" {
		return fmt.Errorf("can't enable metrics dump: file path is not set")
	}
	metricsDump = true
	return nil
}

// Stop dumping metrics to a file when it is logged.
func DisableMetricsDump() {
	logMutex.Lock()
	defer logMutex.Unlock()
	metricsDump = false
}

// Set the metrics logging time duration.
// Need to disable and enable logging to make the change effective.
func SetLoggingDuration(duration time.Duration) error {
	if duration.Seconds() <= 0 {
		return fmt.Errorf("duration must be a positive number")
	}
	logMutex.Lock()
	defer logMutex.Unlock()
	logDuration = duration
	return nil
}

func SetMetricsDumpFilePath(path string) {
	logMutex.Lock()
	defer logMutex.Unlock()
	metricsDumpFilePath = path
}

func LoadMetricsFromDump() error {
	logMutex.Lock()
	defer logMutex.Unlock()
	if metricsDumpFilePath == "" {
		return fmt.Errorf("can't load metrics dump: file path is not set")
	}

	b, err := os.ReadFile(metricsDumpFilePath)
	if err != nil {
		return fmt.Errorf("os.ReadFile() failed: %w", err)
	}
	m := &pb.AllMetrics{}
	if err := proto.Unmarshal(b, m); err != nil {
		return fmt.Errorf("proto.Unmarshal() failed: %w", err)
	}

	var loadFromBytes = func() {
		for _, pbGroup := range m.GetGroups() {
			groupName := pbGroup.GetName()
			if groupName == "" {
				continue
			}
			if group := GetMetricGroupByName(groupName); group != nil {
				for _, pbMetric := range pbGroup.GetMetrics() {
					metricName := pbMetric.GetName()
					if metric, ok := group.GetMetric(metricName); ok {
						if counter, ok := metric.(*Counter); ok {
							loadCounterFromMetricPB(counter, pbMetric)
						}
					}
				}
			} else {
				// Register metrics if not exist.
				for _, pbMetric := range pbGroup.GetMetrics() {
					if pbMetric.GetName() == "" {
						continue
					}
					if pbMetric.GetType() == pb.MetricType_COUNTER {
						RegisterMetric(groupName, pbMetric.GetName(), COUNTER)
					} else if pbMetric.GetType() == pb.MetricType_COUNTER_TIME_SERIES {
						RegisterMetric(groupName, pbMetric.GetName(), COUNTER_TIME_SERIES)
					}
				}
			}
		}
	}

	loadFromBytes()
	loadFromBytes()
	return nil
}

// DumpMetricsNow writes the current metrics to the dump file.
// This function can be called when metrics dump is disabled.
func DumpMetricsNow() error {
	logMutex.Lock()
	if metricsDumpFilePath == "" {
		logMutex.Unlock()
		return fmt.Errorf("can't dump metrics: file path is not set")
	}
	logMutex.Unlock()

	m := &pb.AllMetrics{}
	pbGroups := make([]*pb.MetricGroup, 0)
	metricMap.Range(func(k, v any) bool {
		group := v.(*MetricGroup)
		pbGroup := &pb.MetricGroup{
			Name: proto.String(group.name),
		}
		pbMetrics := make([]*pb.Metric, 0)
		group.metrics.Range(func(k, v any) bool {
			metric := v.(Metric)
			pbMetrics = append(pbMetrics, ToMetricPB(metric))
			return true
		})
		pbGroup.Metrics = pbMetrics
		pbGroups = append(pbGroups, pbGroup)
		return true
	})
	m.Groups = pbGroups
	b, err := proto.Marshal(m)
	if err != nil {
		return fmt.Errorf("proto.Marshal() failed: %w", err)
	}
	if err := os.WriteFile(metricsDumpFilePath, b, 0660); err != nil {
		return fmt.Errorf("os.WriteFile(%q) failed: %w", metricsDumpFilePath, err)
	}
	return nil
}

// ToMetricPB creates a protobuf representation of a metric.
func ToMetricPB(src Metric) *pb.Metric {
	dst := &pb.Metric{}
	dst.Name = proto.String(src.Name())
	switch src.Type() {
	case COUNTER:
		dst.Type = pb.MetricType_COUNTER.Enum()
	case COUNTER_TIME_SERIES:
		dst.Type = pb.MetricType_COUNTER_TIME_SERIES.Enum()
	case GAUGE:
		dst.Type = pb.MetricType_GAUGE.Enum()
	default:
		dst.Type = pb.MetricType_UNSPECIFIED.Enum()
	}
	dst.Value = proto.Int64(src.Load())
	if src.Type() == COUNTER_TIME_SERIES {
		counter := src.(*Counter)
		counter.mu.Lock()
		dst.Value = proto.Int64(counter.value) // Make sure value matches history.
		dst.History = make([]*pb.History, len(counter.history))
		copy(dst.History, counter.history)
		counter.mu.Unlock()
	}
	return dst
}

// FromMetricPB creates a metric from the protobuf.
func FromMetricPB(src *pb.Metric) (Metric, error) {
	if src.GetType() != pb.MetricType_COUNTER && src.GetType() != pb.MetricType_COUNTER_TIME_SERIES && src.GetType() != pb.MetricType_GAUGE {
		return nil, fmt.Errorf("metric type %v is invalid", src.GetType().String())
	}

	var m Metric
	switch src.GetType() {
	case pb.MetricType_COUNTER:
		m = &Counter{
			name: src.GetName(),
		}
		loadCounterFromMetricPB(m.(*Counter), src)
	case pb.MetricType_COUNTER_TIME_SERIES:
		m = &Counter{
			name:       src.GetName(),
			timeSeries: true,
		}
		loadCounterFromMetricPB(m.(*Counter), src)
	case pb.MetricType_GAUGE:
		m = &Gauge{
			name: src.GetName(),
		}
		loadGaugeFromMetricPB(m.(*Gauge), src)
	}
	return m, nil
}

// LogMetricsNow writes the current metrics to log.
// This function can be called when (periodic) logging is disabled.
func LogMetricsNow() {
	log.Infof("[metrics]")
	list := MetricGroupList{}
	metricMap.Range(func(k, v any) bool {
		group := v.(*MetricGroup)
		if group.IsLoggingEnabled() {
			list = list.Append(group)
		}
		return true
	})
	sort.Sort(list)
	for _, group := range list {
		log.WithFields(group.NewLogFields()).Infof(group.NewLogMsg())
	}
}

func logMetricsLoop() {
	for {
		select {
		case <-logTicker.C:
			LogMetricsNow()
			if metricsDump {
				if err := DumpMetricsNow(); err != nil {
					log.Warnf("DumpMetricsNow() failed: %v", err)
				}
			}
		case <-stopLogging:
			return
		}
	}
}

func loadCounterFromMetricPB(dst *Counter, src *pb.Metric) {
	// Verify the type matches.
	if src.GetName() != dst.Name() {
		return
	}

	if (src.GetType() == pb.MetricType_COUNTER && dst.Type() == COUNTER) || (src.GetType() == pb.MetricType_COUNTER_TIME_SERIES && dst.Type() == COUNTER_TIME_SERIES) {
		delta := mathext.Max(0, src.GetValue()-dst.Load())
		dst.Add(delta)
		dst.history = src.GetHistory()
	}
}

func loadGaugeFromMetricPB(dst *Gauge, src *pb.Metric) {
	// Verify the type matches.
	if src.GetName() != dst.Name() {
		return
	}

	dst.Store(src.GetValue())
}
