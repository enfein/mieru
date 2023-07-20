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
	"sort"
	"sync"
	"time"

	"github.com/enfein/mieru/pkg/log"
)

var ticker *time.Ticker
var logDuration time.Duration
var done chan struct{}
var mutex sync.Mutex

func init() {
	logDuration = time.Minute
	done = make(chan struct{})
}

// Enable metrics logging with the given time duration.
func EnableLogging() {
	mutex.Lock()
	defer mutex.Unlock()
	if ticker == nil {
		ticker = time.NewTicker(logDuration)
		go logMetricsLoop()
		log.Infof("enabled metrics logging with duration %v", logDuration)
	}
}

// Disable metrics logging.
func DisableLogging() {
	mutex.Lock()
	defer mutex.Unlock()
	done <- struct{}{}
	if ticker != nil {
		ticker.Stop()
		ticker = nil
		log.Infof("disabled metrics logging")
	}
}

// Set the metrics logging time duration.
func SetLoggingDuration(duration time.Duration) error {
	if duration.Seconds() <= 0 {
		return fmt.Errorf("duration must be a positive number")
	}
	mutex.Lock()
	defer mutex.Unlock()
	logDuration = duration
	return nil
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
		case <-ticker.C:
			LogMetricsNow()
		case <-done:
			return
		}
	}
}
