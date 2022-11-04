package log

import (
	"bytes"
	"testing"
)

type testBufferPool struct {
	buffers []*bytes.Buffer
	get     int
}

func (p *testBufferPool) Get() *bytes.Buffer {
	p.get++
	return new(bytes.Buffer)
}

func (p *testBufferPool) Put(buf *bytes.Buffer) {
	p.buffers = append(p.buffers, buf)
}

func TestLoggerSetBufferPool(t *testing.T) {
	out := &bytes.Buffer{}
	l := New()
	l.SetOutput(out)

	pool := new(testBufferPool)
	l.SetBufferPool(pool)

	l.Infof("test")
	if pool.get != 1 {
		t.Errorf("Logger.SetBufferPool(): The BufferPool.Get() must be called")
	}
	if len(pool.buffers) != 1 {
		t.Errorf("Logger.SetBufferPool(): The BufferPool.Put() must be called")
	}
}
