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

package metrics

const (
	// MetricGroup name format for each user.
	UserMetricGroupFormat = "user - %s"

	UserMetricReadBytes  = "ReadBytes"
	UserMetricWriteBytes = "WriteBytes"
)

var (
	// Max number of connections ever reached.
	MaxConn = RegisterMetric("connections", "MaxConn")

	// Accumulated active open connections.
	ActiveOpens = RegisterMetric("connections", "ActiveOpens")

	// Accumulated passive open connections.
	PassiveOpens = RegisterMetric("connections", "PassiveOpens")

	// Current number of established connections.
	CurrEstablished = RegisterMetric("connections", "CurrEstablished")

	// Number of bytes receive from proxy connections.
	InBytes = RegisterMetric("traffic", "InBytes")

	// Number of bytes send to proxy connections.
	OutBytes = RegisterMetric("traffic", "OutBytes")

	// Number of padding bytes send to proxy connections.
	OutPaddingBytes = RegisterMetric("traffic", "OutPaddingBytes")
)
