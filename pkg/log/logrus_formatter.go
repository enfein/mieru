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

package log

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// CliFormatter is a log formatter that works best for command output.
// It doesn't print time, level, or field data.
type CliFormatter struct{}

func (f *CliFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var buf *bytes.Buffer
	if entry.Buffer != nil {
		buf = entry.Buffer
	} else {
		buf = &bytes.Buffer{}
	}

	buf.WriteString(entry.Message)
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// DaemonFormatter is the a log formatter that is suitable for daemon.
type DaemonFormatter struct{}

func (f *DaemonFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	userData := make(logrus.Fields)
	for k, v := range entry.Data {
		userData[k] = v
	}
	userKeys := make([]string, 0, len(userData))
	for k := range userData {
		userKeys = append(userKeys, k)
	}

	var fileInfo, funcInfo string

	orderedKeys := make([]string, 0, 5+len(userData))
	orderedKeys = append(orderedKeys, logrus.FieldKeyTime)
	orderedKeys = append(orderedKeys, logrus.FieldKeyLevel)
	orderedKeys = append(orderedKeys, logrus.FieldKeyMsg)
	if entry.HasCaller() {
		fileInfo = fmt.Sprintf("%s:%d", entry.Caller.File, entry.Caller.Line)
		funcInfo = entry.Caller.Function
		orderedKeys = append(orderedKeys, logrus.FieldKeyFile)
		orderedKeys = append(orderedKeys, logrus.FieldKeyFunc)
	}
	sort.Strings(userKeys)
	orderedKeys = append(orderedKeys, userKeys...)

	var buf *bytes.Buffer
	if entry.Buffer != nil {
		buf = entry.Buffer
	} else {
		buf = &bytes.Buffer{}
	}

	for _, key := range orderedKeys {
		var value string
		switch {
		case key == logrus.FieldKeyTime:
			value = entry.Time.Format(time.RFC3339)
		case key == logrus.FieldKeyLevel:
			value = strings.ToUpper(entry.Level.String())
		case key == logrus.FieldKeyMsg:
			value = entry.Message
		case key == logrus.FieldKeyFile && entry.HasCaller():
			value = fileInfo
		case key == logrus.FieldKeyFunc && entry.HasCaller():
			value = funcInfo
		default:
			value = fmt.Sprintf("%v=%v", key, userData[key])
		}

		if buf.Len() > 0 {
			// Add a space to separate from the previous field.
			buf.WriteString(" ")
		}
		buf.WriteString(value)
	}
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// NilFormatter prints no log. It disables logging.
type NilFormatter struct{}

func (f *NilFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	return []byte{}, nil
}
