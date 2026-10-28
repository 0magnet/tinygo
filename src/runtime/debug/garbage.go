// Package debug is a very partially implemented package to allow compilation.
package debug

import (
	"time"
	_ "unsafe"
)

type GCStats struct {
	LastGC         time.Time
	NumGC          int64
	PauseTotal     time.Duration
	Pause          []time.Duration
	PauseEnd       []time.Time
	PauseQuantiles []time.Duration
}

func ReadGCStats(stats *GCStats) {
}

// FreeOSMemory forces a collection and returns as much memory to the
// operating system as the garbage collector can.
func FreeOSMemory() {
	freeOSMemory()
}

//go:linkname freeOSMemory runtime.freeOSMemory
func freeOSMemory()

func SetMaxThreads(threads int) int {
	return threads
}

func SetPanicOnFault(enabled bool) bool {
	return enabled
}

// WriteHeapDump writes nothing: TinyGo has no heap dump in Go's format.
// Declared without a body, as Go declares it, it failed to link.
func WriteHeapDump(fd uintptr) {}

func SetTraceback(level string) {
}

// SetMemoryLimit sets a soft limit on the memory of the runtime. Only the Boehm
// GC under the cores scheduler on Linux applies it.
func SetMemoryLimit(limit int64) int64 {
	return setMemoryLimit(limit)
}

//go:linkname setMemoryLimit runtime.setMemoryLimit
func setMemoryLimit(int64) int64
