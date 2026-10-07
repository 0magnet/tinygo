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

func WriteHeapDump(fd uintptr)

func SetTraceback(level string) {
}

func SetMemoryLimit(limit int64) int64 {
	return limit
}
