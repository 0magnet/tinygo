//go:build boehm.threads

package runtime

// bdwgc with its own thread support, under the cores scheduler on Linux. Each
// core thread is registered with bdwgc, so it allocates from thread local free
// lists without a lock, and bdwgc stops the registered threads for a
// collection with its suspend signal. The runtime chains a handler in front of
// bdwgc's that records the stack ranges of the core, and the root callback
// scans them, as bdwgc never scans thread stacks itself here.

import (
	"internal/task"
	"sync/atomic"
	"unsafe"
)

// Collections an empty block stays mapped, MUNMAP_THRESHOLD in builder/bdwgc.go.
const gcUnmapAfter = 3

// Collection events, see GC_EventType in lib/bdwgc/include/gc/gc.h.
const (
	gcEventPreStopWorld   = 6
	gcEventPostStopWorld  = 7
	gcEventPreStartWorld  = 8
	gcEventPostStartWorld = 9
)

type gcCoreCounters struct {
	mallocs uint64
	profile uintptr // bytes until the next heap profile sample
	_       [48]byte
}

var (
	gcCounters [numCPU]gcCoreCounters

	// gcStopGen is bumped before each stop, and a core stamps gcCoreGen when
	// it stops, so the roots of a core that bdwgc did not stop are skipped.
	gcStopGen      atomic.Uint32
	gcCoreGen      [numCPU]atomic.Uint32
	gcWorldStopped bool

	// World stop times, from the stop request until all cores run again.
	gcStopStart  uint64
	gcPauseTotal uint64
	gcLastGC     uint64
	gcTrace      bool
	gcStopAllocd uintptr

	// gcGrow is set after each collection, for gcGrowHeap.
	gcGrow                 atomic.Uint32
	gcStopHeap, gcStopFree uintptr // heap state when the world stopped

	// The divisor from GOGC or SetGCPercent, and the one bdwgc uses.
	gcBaseDivisor, gcDivisor uintptr
	gcUnmapNow               uintptr = gcUnmapAfter
	// The heap size bdwgc may not grow past, or zero.
	gcHeapCap       uintptr
	gcOOMCollecting atomic.Uint32
	// The last pause, and the time from the end of the pause before it.
	gcLastPause, gcLastCycle, gcPrevEnd uint64
)

func gcAllocLock() {}

func gcAllocUnlock() {
	if gcGrow.Load() != 0 {
		gcGrowHeap()
	}
}

// gcGrowHeap sizes the heap after a collection, on the first allocation after
// it. Without incremental mode bdwgc collects when the heap is full, and
// grows it by at most MAXHINCR blocks (8 MB) and then collects again, so a GB
// heap took hundreds of collections to build. It also applies the memory
// limit, which bdwgc does not have.
func gcGrowHeap() {
	if !gcGrow.CompareAndSwap(1, 0) {
		return
	}
	// Garbage is about what was allocated since the previous collection.
	used := gcStopHeap - gcStopFree
	live := used / 2
	if used > gcStopAllocd {
		live = used - gcStopAllocd
	}
	base := gcBaseDivisor
	want := live + 2*live/base
	var heap, free, unmapped, sinceGC, total uintptr
	libgc_get_heap_usage_safe(&heap, &free, &unmapped, &sinceGC, &total)

	div, unmapAfter := base, uintptr(gcUnmapAfter)
	capHeap := uintptr(0)
	if limit := gcMemoryLimit; limit != maxMemoryLimit {
		// What is not heap, such as goroutine stacks, counts against the
		// limit as well, as in Go.
		rss := tinygo_runtime_bdwgc_rss()
		other := uintptr(0)
		if rss > heap {
			other = rss - heap
		}
		room := uintptr(0)
		if uint64(limit) > uint64(other) {
			room = uintptr(limit) - other
		}
		if want > room {
			want = room
			if want < live {
				want = live
			}
			// bdwgc collects after about 2*live/divisor bytes. Collect once
			// the room left is used, unless collections already take more
			// than gcLimitPause percent of the time. Go allows its GC half the
			// CPU, but here a collection stops every core.
			extra := want - live
			if extra < live/64 {
				extra = live / 64
			}
			div = 2 * live / (extra + 1)
			if div > 128 {
				div = 128
			}
			if gcPauseFraction() > float64(gcLimitPause)/100 {
				div = gcDivisor / 2
			}
			if div < base {
				div = base
			}
		}
		// Cap the heap at the room left so that bdwgc collects rather than
		// grow, see gcOutOfMemory, unless collections already take more than
		// gcLimitPause percent of the time.
		capHeap = room
		if floor := live + live/16; capHeap < floor {
			capHeap = floor
		}
		if gcPauseFraction() > float64(gcLimitPause)/100 {
			capHeap = 0
		}
		if heap+other > uintptr(limit)/10*9 {
			// Give empty blocks back after one collection, not three.
			unmapAfter = 1
		}
		if gcTrace {
			println("gc limit", int(limit>>20), "MB rss", int(rss>>20), "MB other", int(other>>20), "MB live", int(live>>20), "MB target", int(want>>20), "MB divisor", int(div))
		}
	}
	if capHeap != gcHeapCap {
		gcHeapCap = capHeap
		libgc_set_max_heap_size(capHeap)
	}
	if div != gcDivisor {
		gcDivisor = div
		libgc_set_free_space_divisor(div)
	}
	if unmapAfter != gcUnmapNow {
		gcUnmapNow = unmapAfter
		tinygo_runtime_bdwgc_set_unmap_threshold(uint32(unmapAfter))
	}
	// Unmapped blocks are mapped again before the heap grows.
	if have := heap + unmapped; want > have {
		libgc_expand_hp(want - have)
		if gcTrace {
			println("gc grow live", int(live>>20), "MB heap", int(want>>20), "MB")
		}
	}
}

// gcOutOfMemory is called when bdwgc returns nil. The memory limit caps the
// heap, so collect, and then lift the cap, since the limit is soft.
func gcOutOfMemory(attempt int) bool {
	if gcHeapCap == 0 {
		return false
	}
	if attempt == 0 {
		// One core collects. The others retry, and wait for it in bdwgc.
		if gcOOMCollecting.CompareAndSwap(0, 1) {
			libgc_gcollect()
			gcOOMCollecting.Store(0)
		}
		return true
	}
	gcHeapCap = 0
	libgc_set_max_heap_size(0)
	return attempt == 1
}

//go:extern tinygo_runtime_bdwgc_limit_pause
var gcLimitPause int32

// gcPauseFraction is the share of the last collection cycle the world was
// stopped.
func gcPauseFraction() float64 {
	if gcLastCycle == 0 {
		return 0
	}
	return float64(gcLastPause) / float64(gcLastCycle)
}

// gcSetDivisor sets the free space divisor chosen by GOGC or SetGCPercent.
// The memory limit may raise the divisor bdwgc uses above it.
func gcSetDivisor(d uintptr) uintptr {
	old := gcBaseDivisor
	gcBaseDivisor, gcDivisor = d, d
	libgc_set_free_space_divisor(d)
	return old
}

func gcGetDivisor() uintptr {
	return gcBaseDivisor
}

func gcCountMalloc() {
	gcCounters[currentCPU()].mallocs++
}

func gcMallocCount() uint64 {
	var n uint64
	for i := range gcCounters {
		n += gcCounters[i].mallocs
	}
	return n
}

// gcProfileAlloc counts down per core, and takes gcLock only to record a
// sample.
func gcProfileAlloc(ptr unsafe.Pointer, size, pc uintptr) {
	c := &gcCounters[currentCPU()]
	if size < c.profile {
		c.profile -= size
		return
	}
	c.profile = memProfRate
	gcLock.Lock()
	memProfTake(ptr, size, pc)
	gcLock.Unlock()
}

func gcInitThreads() {
	libgc_allow_register_threads()
	gcTrace = tinygo_runtime_bdwgc_threads_init() != 0
	gcBaseDivisor = libgc_get_free_space_divisor()
	gcDivisor = gcBaseDivisor
	if limit := tinygo_runtime_bdwgc_memory_limit(); limit >= 0 {
		gcMemoryLimit = limit
	}
	for i := range gcCounters {
		gcCounters[i].profile = memProfRate
	}
}

// coresInitGC chains the core stop handler in front of bdwgc's.
func coresInitGC() {
	tinygo_runtime_bdwgc_chain_suspend(&coreThreads[0])
}

// coresRegisterGC registers a secondary core thread with bdwgc, before it runs
// any goroutine.
func coresRegisterGC(cpu uint32) {
	var sb libgc_stack_base
	sb.mem_base = coreStackTop(cpu)
	if r := libgc_register_my_thread(&sb); r != 0 {
		runtimeFatal("gc: cannot register a core thread")
	}
}

// gcEvent runs with the bdwgc lock held, so it must not allocate. Even
// nanotime may allocate without optimization.
//
//export tinygo_runtime_bdwgc_event
func gcEvent(event int32) {
	switch event {
	case gcEventPreStopWorld:
		gcStopStart = tinygo_runtime_bdwgc_clock(0)
		gcStopAllocd = libgc_get_bytes_since_gc()
		gcStopHeap = libgc_get_heap_size()
		gcStopFree = libgc_get_free_bytes()
		// No core may start or end a goroutine while their stacks are
		// scanned. Nothing allocates while holding this lock.
		task.LockStacks()
		gcStopGen.Add(1)
	case gcEventPostStopWorld:
		gcWorldStopped = true
	case gcEventPreStartWorld:
		gcReleaseIdleStacks()
		gcWorldStopped = false
		task.UnlockStacks()
	case gcEventPostStartWorld:
		gcGrow.Store(1)
		end := tinygo_runtime_bdwgc_clock(0)
		d := end - gcStopStart
		gcLastPause = d
		if gcPrevEnd != 0 {
			gcLastCycle = end - gcPrevEnd
		}
		gcPrevEnd = end
		gcPauseTotal += d
		gcLastGC = tinygo_runtime_bdwgc_clock(1)
		if gcTrace {
			println("gc pause", d/1000, "us heap", int(libgc_get_heap_size()>>20), "MB free", int(libgc_get_free_bytes()>>20), "MB allocd", int(gcStopAllocd>>20), "MB divisor", int(libgc_get_free_space_divisor()))
		}
	}
}

//export tinygo_cores_gc_record
func coresGCRecord() {
	gcRecordCore()
	gcCoreGen[currentCPU()].Store(gcStopGen.Load())
}

func gcMarkReachable() {
	if !gcWorldStopped {
		// bdwgc pushes the roots again with the world stopped.
		scanCurrentStack()
		markGlobals()
		return
	}
	me := currentCPU()
	gen := gcStopGen.Load()
	for i := uint32(0); i < numCPU; i++ {
		if i != me && gcCoreGen[i].Load() == gen {
			markStoppedCore(i)
		}
	}
	scanCurrentStack()
	task.MarkParkedStacks(markRoots)
	markSystemStack(me)
	markGlobals()
}

func gcReadPauses(m *MemStats) {
	m.PauseTotalNs = gcPauseTotal
	m.LastGC = gcLastGC
}

// bdwgc resumes the world itself.
func gcResumeWorld() {}

type libgc_stack_base struct {
	mem_base uintptr
}

//export GC_get_heap_usage_safe
func libgc_get_heap_usage_safe(heap, free, unmapped, sinceGC, total *uintptr)

//export GC_expand_hp
func libgc_expand_hp(bytes uintptr) int32

//export GC_get_heap_size
func libgc_get_heap_size() uintptr

//export GC_get_free_bytes
func libgc_get_free_bytes() uintptr

//export GC_get_bytes_since_gc
func libgc_get_bytes_since_gc() uintptr

//export GC_allow_register_threads
func libgc_allow_register_threads()

//export GC_register_my_thread
func libgc_register_my_thread(*libgc_stack_base) int32

//export tinygo_runtime_bdwgc_threads_init
func tinygo_runtime_bdwgc_threads_init() int32

//export GC_set_max_heap_size
func libgc_set_max_heap_size(uintptr)

//export tinygo_runtime_bdwgc_rss
func tinygo_runtime_bdwgc_rss() uintptr

//export tinygo_runtime_bdwgc_memory_limit
func tinygo_runtime_bdwgc_memory_limit() int64

//export tinygo_runtime_bdwgc_set_unmap_threshold
func tinygo_runtime_bdwgc_set_unmap_threshold(n uint32)

//export tinygo_runtime_bdwgc_clock
func tinygo_runtime_bdwgc_clock(realtime int32) uint64

//export tinygo_runtime_bdwgc_chain_suspend
func tinygo_runtime_bdwgc_chain_suspend(thread *threadID)

// gcAllocatorLock takes the bdwgc allocator lock, which keeps a collection
// from starting.
func gcAllocatorLock() { libgc_alloc_lock() }

func gcAllocatorUnlock() { libgc_alloc_unlock() }

//export GC_alloc_lock
func libgc_alloc_lock()

//export GC_alloc_unlock
func libgc_alloc_unlock()
