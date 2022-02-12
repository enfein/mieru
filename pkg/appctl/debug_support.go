package appctl

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"

	"github.com/enfein/mieru/pkg/stderror"
)

var (
	cpuProfileFile *os.File
	mu             sync.Mutex
)

func getThreadDump() []byte {
	mu.Lock()
	defer mu.Unlock()
	buf := make([]byte, 16384)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 4*len(buf))
	}
	return buf
}

func startCPUProfile(filePath string) error {
	mu.Lock()
	defer mu.Unlock()
	if filePath == "" {
		return fmt.Errorf("file path is empty")
	}
	if cpuProfileFile != nil {
		return stderror.ErrAlreadyStarted
	}
	cpuProfileFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("os.Create() failed: %w", err)
	}
	if err := pprof.StartCPUProfile(cpuProfileFile); err != nil {
		return fmt.Errorf("pprof.StartCPUProfile() failed: %w", err)
	}
	return nil
}

func stopCPUProfile() {
	mu.Lock()
	defer mu.Unlock()
	pprof.StopCPUProfile()
	if cpuProfileFile != nil {
		cpuProfileFile.Close()
	}
	cpuProfileFile = nil
}

func getHeapProfile(filePath string) error {
	mu.Lock()
	defer mu.Unlock()
	if filePath == "" {
		return fmt.Errorf("file path is empty")
	}
	heapFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("os.Create() failed: %w", err)
	}
	defer heapFile.Close()
	runtime.GC()
	if err := pprof.WriteHeapProfile(heapFile); err != nil {
		return fmt.Errorf("pprof.WriteHeapProfile() failed: %w", err)
	}
	return nil
}
