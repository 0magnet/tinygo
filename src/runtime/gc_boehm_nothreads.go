//go:build gc.boehm && !boehm.threads

package runtime

import "unsafe"

var gcMallocs uint64

func gcAllocLock() {
	gcLock.Lock()
}

func gcAllocUnlock() {
	gcResumeWorld()
	gcLock.Unlock()
}

func gcCountMalloc() {
	gcMallocs++
}

func gcMallocCount() uint64 {
	return gcMallocs
}

func gcProfileAlloc(ptr unsafe.Pointer, size, pc uintptr) {
	memProfRecord(ptr, size, pc)
}

func gcInitThreads() {}

func gcReadPauses(m *MemStats) {}
