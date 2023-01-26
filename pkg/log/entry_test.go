package log

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEntryWithError(t *testing.T) {
	defer func() {
		ErrorKey = "error"
	}()

	err := fmt.Errorf("kaboom at layer %d", 4711)

	if err != WithError(err).Data["error"] {
		t.FailNow()
	}

	logger := New()
	logger.Out = &bytes.Buffer{}
	entry := NewEntry(logger)

	if err != entry.WithError(err).Data["error"] {
		t.FailNow()
	}

	ErrorKey = "err"

	if err != entry.WithError(err).Data["err"] {
		t.FailNow()
	}

}

func TestEntryWithContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), "foo", "bar")

	if ctx != WithContext(ctx).Context {
		t.FailNow()
	}

	logger := New()
	logger.Out = &bytes.Buffer{}
	entry := NewEntry(logger)

	if ctx != entry.WithContext(ctx).Context {
		t.FailNow()
	}
}

func TestEntryWithContextCopiesData(t *testing.T) {
	// Initialize a parent Entry object with a key/value set in its Data map
	logger := New()
	logger.Out = &bytes.Buffer{}
	parentEntry := NewEntry(logger).WithField("parentKey", "parentValue")

	// Create two children Entry objects from the parent in different contexts
	ctx1 := context.WithValue(context.Background(), "foo", "bar")
	childEntry1 := parentEntry.WithContext(ctx1)
	if ctx1 != childEntry1.Context {
		t.FailNow()
	}

	ctx2 := context.WithValue(context.Background(), "bar", "baz")
	childEntry2 := parentEntry.WithContext(ctx2)
	if ctx2 != childEntry2.Context {
		t.FailNow()
	}
	if ctx1 == ctx2 {
		t.FailNow()
	}

	// Ensure that data set in the parent Entry are preserved to both children
	if childEntry1.Data["parentKey"] != "parentValue" || childEntry2.Data["parentKey"] != "parentValue" {
		t.FailNow()
	}

	// Modify data stored in the child entry
	childEntry1.Data["childKey"] = "childValue"

	// Verify that data is successfully stored in the child it was set on
	val, exists := childEntry1.Data["childKey"]
	if !exists || val != "childValue" {
		t.FailNow()
	}

	// Verify that the data change to child 1 has not affected its sibling
	_, exists = childEntry2.Data["childKey"]
	if exists {
		t.FailNow()
	}

	// Verify that the data change to child 1 has not affected its parent
	_, exists = parentEntry.Data["childKey"]
	if exists {
		t.FailNow()
	}
}

func TestEntryWithTimeCopiesData(t *testing.T) {
	// Initialize a parent Entry object with a key/value set in its Data map
	logger := New()
	logger.Out = &bytes.Buffer{}
	parentEntry := NewEntry(logger).WithField("parentKey", "parentValue")

	// Create two children Entry objects from the parent with two different times
	childEntry1 := parentEntry.WithTime(time.Now().AddDate(0, 0, 1))
	childEntry2 := parentEntry.WithTime(time.Now().AddDate(0, 0, 2))

	// Ensure that data set in the parent Entry are preserved to both children
	if childEntry1.Data["parentKey"] != "parentValue" || childEntry2.Data["parentKey"] != "parentValue" {
		t.FailNow()
	}

	// Modify data stored in the child entry
	childEntry1.Data["childKey"] = "childValue"

	// Verify that data is successfully stored in the child it was set on
	val, exists := childEntry1.Data["childKey"]
	if !exists || val != "childValue" {
		t.FailNow()
	}

	// Verify that the data change to child 1 has not affected its sibling
	_, exists = childEntry2.Data["childKey"]
	if exists {
		t.FailNow()
	}

	// Verify that the data change to child 1 has not affected its parent
	_, exists = parentEntry.Data["childKey"]
	if exists {
		t.FailNow()
	}
}

func TestEntryPanicln(t *testing.T) {
	errBoom := fmt.Errorf("boom time")

	defer func() {
		p := recover()
		if p == nil {
			t.FailNow()
		}

		switch pVal := p.(type) {
		case *Entry:
			if pVal.Message != "kaboom" {
				t.FailNow()
			}
			if pVal.Data["err"] != errBoom {
				t.FailNow()
			}
		default:
			t.Fatalf("want type *Entry, got %T: %#v", pVal, pVal)
		}
	}()

	logger := New()
	logger.Out = &bytes.Buffer{}
	entry := NewEntry(logger)
	entry.WithField("err", errBoom).Panicln("kaboom")
}

func TestEntryPanicf(t *testing.T) {
	errBoom := fmt.Errorf("boom again")

	defer func() {
		p := recover()
		if p == nil {
			t.FailNow()
		}

		switch pVal := p.(type) {
		case *Entry:
			if pVal.Message != "kaboom true" {
				t.FailNow()
			}
			if pVal.Data["err"] != errBoom {
				t.FailNow()
			}
		default:
			t.Fatalf("want type *Entry, got %T: %#v", pVal, pVal)
		}
	}()

	logger := New()
	logger.Out = &bytes.Buffer{}
	entry := NewEntry(logger)
	entry.WithField("err", errBoom).Panicf("kaboom %v", true)
}

func TestEntryPanic(t *testing.T) {
	errBoom := fmt.Errorf("boom again")

	defer func() {
		p := recover()
		if p == nil {
			t.FailNow()
		}

		switch pVal := p.(type) {
		case *Entry:
			if pVal.Message != "kaboom" {
				t.FailNow()
			}
			if pVal.Data["err"] != errBoom {
				t.FailNow()
			}
		default:
			t.Fatalf("want type *Entry, got %T: %#v", pVal, pVal)
		}
	}()

	logger := New()
	logger.Out = &bytes.Buffer{}
	entry := NewEntry(logger)
	entry.WithField("err", errBoom).Panic("kaboom")
}

const (
	badMessage   = "this is going to panic"
	panicMessage = "this is broken"
)

type panickyHook struct{}

func (p *panickyHook) Levels() []Level {
	return []Level{InfoLevel}
}

func (p *panickyHook) Fire(entry *Entry) error {
	if entry.Message == badMessage {
		panic(panicMessage)
	}

	return nil
}

func TestEntryWithIncorrectField(t *testing.T) {

	fn := func() {}

	e := Entry{Logger: New()}
	eWithFunc := e.WithFields(Fields{"func": fn})
	eWithFuncPtr := e.WithFields(Fields{"funcPtr": &fn})

	if eWithFunc.err != `can not add field "func"` || eWithFuncPtr.err != `can not add field "funcPtr"` {
		t.FailNow()
	}

	eWithFunc = eWithFunc.WithField("not_a_func", "it is a string")
	eWithFuncPtr = eWithFuncPtr.WithField("not_a_func", "it is a string")

	if eWithFunc.err != `can not add field "func"` || eWithFuncPtr.err != `can not add field "funcPtr"` {
		t.FailNow()
	}

	eWithFunc = eWithFunc.WithTime(time.Now())
	eWithFuncPtr = eWithFuncPtr.WithTime(time.Now())

	if eWithFunc.err != `can not add field "func"` || eWithFuncPtr.err != `can not add field "funcPtr"` {
		t.FailNow()
	}
}

func TestEntryLogfLevel(t *testing.T) {
	logger := New()
	buffer := &bytes.Buffer{}
	logger.Out = buffer
	logger.SetLevel(InfoLevel)
	entry := NewEntry(logger)

	entry.Logf(DebugLevel, "%s", "debug")
	if strings.Contains(buffer.String(), "debug") {
		t.FailNow()
	}

	entry.Logf(WarnLevel, "%s", "warn")
	if !strings.Contains(buffer.String(), "warn") {
		t.FailNow()
	}
}

func TestEntryReportCallerRace(t *testing.T) {
	logger := New()
	entry := NewEntry(logger)

	// Logging before SetReportCaller has the highest chance of causing a race condition
	// to be detected, but doing it twice just to increase the likelihood of detecting the race.
	go func() {
		entry.Print("should not race")
	}()
	go func() {
		logger.SetReportCaller(true)
	}()
	go func() {
		entry.Print("should not race")
	}()
}

func TestEntryFormatterRace(t *testing.T) {
	logger := New()
	entry := NewEntry(logger)

	// Logging before SetReportCaller has the highest chance of causing a race condition
	// to be detected, but doing it twice just to increase the likelihood of detecting the race.
	go func() {
		entry.Print("should not race")
	}()
	go func() {
		logger.SetFormatter(&CliFormatter{})
	}()
	go func() {
		entry.Print("should not race")
	}()
}
