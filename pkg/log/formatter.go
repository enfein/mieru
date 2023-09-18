package log

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Default key names for the default fields.
const (
	defaultTimestampFormat = time.RFC3339
	FieldKeyMsg            = "msg"
	FieldKeyLevel          = "level"
	FieldKeyTime           = "time"
	FieldKeyFunc           = "func"
	FieldKeyFile           = "file"
)

// LogPrefix is a fixed string printed at the beginning of each line
// with DaemonFormatter. Set it as a build time variable to help debug
// the program.
var LogPrefix = ""

// The Formatter interface is used to implement a custom Formatter. It takes an
// `Entry`. It exposes all the fields, including the default ones:
//
// * `entry.Data["msg"]`. The message passed from Info, Warn, Error ..
// * `entry.Data["time"]`. The timestamp.
// * `entry.Data["level"]. The level the entry was logged at.
//
// Any additional fields added with `WithField` or `WithFields` are also in
// `entry.Data`. Format is expected to return an array of bytes which are then
// logged to `logger.Out`.
type Formatter interface {
	Format(*Entry) ([]byte, error)
}

// CliFormatter is a log formatter that works best for command output.
// It doesn't print log prefix, time, level, or field data.
type CliFormatter struct{}

func (f *CliFormatter) Format(entry *Entry) ([]byte, error) {
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
type DaemonFormatter struct {
	NoTimestamp bool
}

func (f *DaemonFormatter) Format(entry *Entry) ([]byte, error) {
	userData := make(Fields)
	for k, v := range entry.Data {
		userData[k] = v
	}
	userKeys := make([]string, 0, len(userData))
	for k := range userData {
		userKeys = append(userKeys, k)
	}

	var fileInfo, funcInfo string

	orderedKeys := make([]string, 0, 5+len(userData))
	if !f.NoTimestamp {
		orderedKeys = append(orderedKeys, FieldKeyTime)
	}
	orderedKeys = append(orderedKeys, FieldKeyLevel)
	orderedKeys = append(orderedKeys, FieldKeyMsg)
	if entry.HasCaller() {
		fileInfo = fmt.Sprintf("%s:%d", entry.Caller.File, entry.Caller.Line)
		funcInfo = entry.Caller.Function
		orderedKeys = append(orderedKeys, FieldKeyFile)
		orderedKeys = append(orderedKeys, FieldKeyFunc)
	}
	sort.Strings(userKeys)
	orderedKeys = append(orderedKeys, userKeys...)

	var buf *bytes.Buffer
	if entry.Buffer != nil {
		buf = entry.Buffer
	} else {
		buf = &bytes.Buffer{}
	}

	buf.WriteString(LogPrefix)
	for _, key := range orderedKeys {
		var value string
		switch {
		case key == FieldKeyTime:
			value = entry.Time.Format(time.RFC3339)
		case key == FieldKeyLevel:
			value = strings.ToUpper(entry.Level.String())
		case key == FieldKeyMsg:
			value = entry.Message
		case key == FieldKeyFile && entry.HasCaller():
			value = fileInfo
		case key == FieldKeyFunc && entry.HasCaller():
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

func (f *NilFormatter) Format(entry *Entry) ([]byte, error) {
	return []byte{}, nil
}
