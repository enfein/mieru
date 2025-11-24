// Copyright (C) 2024  mieru authors
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
	"os"
	"strings"
	"testing"
	"time"
)

func TestCliFormatter(t *testing.T) {
	SetFormatter(&CliFormatter{})
	SetLevel("TRACE")
	var buf bytes.Buffer
	var msg string
	SetOutput(&buf)
	defer func() {
		SetLevel("INFO")
		SetOutput(os.Stdout)
	}()

	Tracef("This is a test message")
	msg = buf.String()
	checkLogLevel(t, msg, "TRACE", false)
	buf.Reset()

	Debugf("This is a test message")
	msg = buf.String()
	checkLogLevel(t, msg, "DEBUG", false)
	buf.Reset()

	Infof("This is a test message")
	msg = buf.String()
	checkLogLevel(t, msg, "INFO", false)
	buf.Reset()

	Warnf("This is a test message")
	msg = buf.String()
	checkLogLevel(t, msg, "WARNING", false)
	buf.Reset()

	Errorf("This is a test message")
	msg = buf.String()
	checkLogLevel(t, msg, "ERROR", false)
	buf.Reset()
}

func TestDaemonFormatter(t *testing.T) {
	SetFormatter(&DaemonFormatter{})
	SetLevel("TRACE")
	var buf bytes.Buffer
	var msg string
	SetOutput(&buf)
	defer func() {
		SetFormatter(&CliFormatter{})
		SetLevel("INFO")
		SetOutput(os.Stdout)
	}()

	Tracef("This is a test message")
	msg = buf.String()
	checkLogTimestamp(t, msg)
	checkLogLevel(t, msg, "TRACE", true)
	buf.Reset()

	Debugf("This is a test message")
	msg = buf.String()
	checkLogTimestamp(t, msg)
	checkLogLevel(t, msg, "DEBUG", true)
	buf.Reset()

	Infof("This is a test message")
	msg = buf.String()
	checkLogTimestamp(t, msg)
	checkLogLevel(t, msg, "INFO", true)
	buf.Reset()

	Warnf("This is a test message")
	msg = buf.String()
	checkLogTimestamp(t, msg)
	checkLogLevel(t, msg, "WARNING", true)
	buf.Reset()

	Errorf("This is a test message")
	msg = buf.String()
	checkLogTimestamp(t, msg)
	checkLogLevel(t, msg, "ERROR", true)
	buf.Reset()
}

func TestDaemonFormatterOptions(t *testing.T) {
	t.Run("NoPrefix", func(t *testing.T) {
		// Set a log prefix to test that it's not printed
		oldPrefix := LogPrefix
		LogPrefix = "[TEST_PREFIX] "
		defer func() {
			LogPrefix = oldPrefix
		}()

		SetFormatter(&DaemonFormatter{NoPrefix: true})
		SetLevel("INFO")
		var buf bytes.Buffer
		SetOutput(&buf)
		defer func() {
			SetFormatter(&CliFormatter{})
			SetLevel("INFO")
			SetOutput(os.Stdout)
		}()

		Infof("This is a test message")
		msg := buf.String()
		if strings.Contains(msg, "[TEST_PREFIX]") {
			t.Errorf("Log prefix should not be printed with NoPrefix: %q", msg)
		}
		if !strings.Contains(msg, "This is a test message") {
			t.Errorf("Log message should still be printed: %q", msg)
		}
	})

	t.Run("NoLevel", func(t *testing.T) {
		SetFormatter(&DaemonFormatter{NoLevel: true})
		SetLevel("INFO")
		var buf bytes.Buffer
		SetOutput(&buf)
		defer func() {
			SetFormatter(&CliFormatter{})
			SetLevel("INFO")
			SetOutput(os.Stdout)
		}()

		Infof("This is a test message")
		msg := buf.String()
		checkLogLevel(t, msg, "INFO", false)
		if !strings.Contains(msg, "This is a test message") {
			t.Errorf("Log message should still be printed: %q", msg)
		}
		buf.Reset()

		Errorf("This is a test message")
		msg = buf.String()
		checkLogLevel(t, msg, "ERROR", false)
		if !strings.Contains(msg, "This is a test message") {
			t.Errorf("Log message should still be printed: %q", msg)
		}
	})
}

func TestNilFormatter(t *testing.T) {
	SetFormatter(&NilFormatter{})
	SetLevel("TRACE")
	var buf bytes.Buffer
	var msg string
	SetOutput(&buf)
	defer func() {
		SetFormatter(&CliFormatter{})
		SetLevel("INFO")
		SetOutput(os.Stdout)
	}()

	Tracef("This is a test message")
	Debugf("This is a test message")
	Infof("This is a test message")
	Warnf("This is a test message")
	Errorf("This is a test message")
	msg = buf.String()
	if len(msg) > 0 {
		t.Errorf("Got unexpected log printed with NilFormatter: %q", msg)
	}
}

func checkLogLevel(t *testing.T, s, level string, shouldPresent bool) {
	t.Helper()
	if shouldPresent {
		if !strings.Contains(s, level) {
			t.Errorf("%q should be printed in %q", level, s)
		}
	} else {
		if strings.Contains(s, level) {
			t.Errorf("%q should not be printed in %q", level, s)
		}
	}
}

func checkLogTimestamp(t *testing.T, s string) {
	t.Helper()
	parts := strings.Split(s, " ")
	if len(parts) == 0 {
		t.Errorf("Invalid log message: %q", s)
		return
	}
	_, err := time.Parse(time.RFC3339, parts[0])
	if err != nil {
		t.Errorf("Invalid timestamp: %s, %v", parts[0], err)
	}
}
