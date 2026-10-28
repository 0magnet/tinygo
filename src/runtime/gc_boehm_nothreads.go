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

func gcSetDivisor(d uintptr) uintptr {
	old := libgc_get_free_space_divisor()
	libgc_set_free_space_divisor(d)
	return old
}

func gcGetDivisor() uintptr {
	return libgc_get_free_space_divisor()
}

func gcOutOfMemory(attempt int) bool { return false }

func gcAllocatorLock()   {}
func gcAllocatorUnlock() {}

const gcProcs = 1

// gcMarked returns what the last collection marked and what bdwgc allocates
// before the next one. Every bdwgc call holds gcLock, as the caller does.
func gcMarked() (live, trigger uintptr) {
	live = libgc_marked(&trigger)
	return
}

func gcHeapCapBytes() uintptr { return 0 }

// gcReadPauseHist reports false: pauses are only timed with thread support.
func gcReadPauseHist(pauses []uint64) (uint64, bool) { return 0, false }

func gcCPUFraction() float64 { return 0 }
