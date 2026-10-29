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
	gcEventReclaimEnd     = 4
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

	// gcGrow is set by gcPace after each collection, for gcGrowHeap.
	gcGrow atomic.Uint32
	// What gcPace chose for gcGrowHeap to apply: the heap goal, the heap cap
	// and how many collections a free block stays mapped.
	gcWant, gcCapNext, gcUnmapNext uintptr

	gcUnmapNow uintptr = gcUnmapAfter
	// The heap size bdwgc may not grow past, or zero.
	gcHeapCap       uintptr
	gcOOMCollecting atomic.Uint32
	// The last pause, and the time from the end of the pause before it.
	gcLastPause, gcLastCycle, gcPrevEnd uint64
	// What the last collection marked, and what bdwgc allocates before the
	// next, for runtime/metrics.
	gcLiveBytes, gcTriggerBytes uintptr
	gcPauseHist                 [gcPauseBuckets]uint64
)

func gcAllocLock() {}

func gcAllocUnlock() {
	if gcGrow.Load() != 0 {
		gcGrowHeap()
	}
}

// The least that may be allocated between collections at GOGC=100, as Go's
// heapMinimum.
const gcMinTrigger = 4 << 20

//go:extern tinygo_runtime_bdwgc_gc_percent
var gcPercent int32

// gcPace sets what may be allocated before the next collection, GOGC percent
// of the bytes the last one marked, as Go does. Without it bdwgc collects
// only when an allocation finds no free block, so the heap filled up to
// whatever size it had grown to. Near the memory limit the trigger shrinks,
// which bdwgc does not do. bdwgc calls it once marking finished, with its
// allocator lock held, so it must not allocate or take that lock.
//
//export tinygo_runtime_bdwgc_pace
func gcPace() {
	var bdwgcTrigger uintptr
	live := libgc_marked(&bdwgcTrigger)
	pct := uint64(gcPercent)
	t := uint64(live) * pct / 100
	if max := uint64(^uintptr(0) / 4); t > max {
		t = max
	}
	trigger := uintptr(t)
	if min := uintptr(gcMinTrigger * pct / 100); trigger < min {
		trigger = min
	}
	want := live + trigger

	capHeap, unmapAfter := uintptr(0), uintptr(gcUnmapAfter)
	if limit := gcMemoryLimit; limit != maxMemoryLimit {
		// What is not heap, such as goroutine stacks, counts against the
		// limit as well, as in Go.
		heap := libgc_get_heap_size()
		rss := tinygo_runtime_bdwgc_rss()
		other := uintptr(0)
		if rss > heap {
			other = rss - heap
		}
		room := uintptr(0)
		if uint64(limit) > uint64(other) {
			room = uintptr(limit) - other
		}
		slow := gcPauseFraction() > float64(gcLimitPause)/100
		if want > room {
			// Collect once the room left is used, unless collections
			// already take more than gcLimitPause percent of the time. Go
			// allows its GC half the CPU, but here a collection stops every
			// core.
			t = 0
			if room > live {
				t = uint64(room - live)
			}
			if t < uint64(live/64) {
				t = uint64(live / 64)
			}
			if slow && gcTriggerBytes != 0 {
				t = 2 * uint64(gcTriggerBytes)
			}
			if t < uint64(trigger) {
				trigger = uintptr(t)
			}
			want = live + trigger
		}
		// Cap the heap at the room left so that bdwgc collects rather than
		// grow, see gcOutOfMemory, unless collections are too slow.
		capHeap = room
		if floor := live + live/16; capHeap < floor {
			capHeap = floor
		}
		if slow {
			capHeap = 0
		}
		if heap+other > uintptr(limit)/10*9 {
			// Give empty blocks back after one collection, not three.
			unmapAfter = 1
		}
		if gcTrace {
			println("gc limit", int(limit>>20), "MB rss", int(rss>>20), "MB other", int(other>>20), "MB live", int(live>>20), "MB target", int(want>>20), "MB")
		}
	}
	tinygo_runtime_bdwgc_set_pace(trigger)
	gcLiveBytes, gcTriggerBytes = live, trigger
	gcWant, gcCapNext, gcUnmapNext = want, capHeap, unmapAfter
	gcGrow.Store(1)
}

// gcGrowHeap applies what gcPace chose, on the first allocation after a
// collection, as it needs calls that take the allocator lock. Without
// incremental mode bdwgc collects when the heap is full, and grows it by at
// most MAXHINCR blocks (8 MB) and then collects again, so a GB heap took
// hundreds of collections to build. The heap is grown to the goal at once.
func gcGrowHeap() {
	if !gcGrow.CompareAndSwap(1, 0) {
		return
	}
	gcAllocatorLock()
	want, capHeap, unmapAfter := gcWant, gcCapNext, gcUnmapNext
	// Under the allocator lock, so that gcHeapCap and the bdwgc cap stay the
	// same while gcOutOfMemory lifts the cap on another core.
	if capHeap != gcHeapCap {
		gcHeapCap = capHeap
		libgc_set_max_heap_size(capHeap)
	}
	gcAllocatorUnlock()

	if unmapAfter != gcUnmapNow {
		gcUnmapNow = unmapAfter
		tinygo_runtime_bdwgc_set_unmap_threshold(uint32(unmapAfter))
	}
	// Unmapped blocks are mapped again before the heap grows.
	var heap, free, unmapped, sinceGC, total uintptr
	libgc_get_heap_usage_safe(&heap, &free, &unmapped, &sinceGC, &total)
	if have := heap + unmapped; want > have {
		libgc_expand_hp(want - have)
		if gcTrace {
			println("gc grow heap", int(want>>20), "MB")
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
	// gcGrowHeap may set the cap again after a collection on another core,
	// so lift it each time.
	gcAllocatorLock()
	gcHeapCap = 0
	libgc_set_max_heap_size(0)
	gcAllocatorUnlock()
	return attempt < 16
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

// gcSetPercent sets GOGC, which paces collections from the next one.
func gcSetPercent(percent int32) int32 {
	old := gcPercent
	if percent > 0 {
		gcPercent = percent
	}
	return old
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
	case gcEventReclaimEnd:
		gcLiveBytes = libgc_marked(&gcTriggerBytes)
	case gcEventPreStopWorld:
		gcStopStart = tinygo_runtime_bdwgc_clock(0)
		gcStopAllocd = libgc_get_bytes_since_gc()
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
		end := tinygo_runtime_bdwgc_clock(0)
		d := end - gcStopStart
		gcLastPause = d
		if gcPrevEnd != 0 {
			gcLastCycle = end - gcPrevEnd
		}
		gcPrevEnd = end
		gcPauseTotal += d
		gcPauseHist[gcPauseBucket(d)]++
		gcLastGC = tinygo_runtime_bdwgc_clock(1)
		if gcTrace {
			println("gc pause", d/1000, "us heap", int(libgc_get_heap_size()>>20), "MB free", int(libgc_get_free_bytes()>>20), "MB allocd", int(gcStopAllocd>>20), "MB trigger", int(gcTriggerBytes>>20), "MB")
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

// gcProcs is how many cores run goroutines, which all stop for a pause.
const gcProcs = numCPU

// gcMarked returns what the last collection marked and what bdwgc allocates
// before the next one, as recorded when it finished sweeping.
func gcMarked() (live, trigger uintptr) {
	return gcLiveBytes, gcTriggerBytes
}

func gcHeapCapBytes() uintptr { return gcHeapCap }

// gcReadPauseHist copies the pause histogram to pauses and returns the total
// pause time.
func gcReadPauseHist(pauses []uint64) (uint64, bool) {
	gcAllocatorLock()
	copy(pauses, gcPauseHist[:])
	total := gcPauseTotal
	gcAllocatorUnlock()
	return total, true
}

// gcCPUFraction is the share of the available CPU time spent in pauses.
func gcCPUFraction() float64 {
	if d := nanotime() - metricsStart; d > 0 {
		return float64(gcPauseTotal) / float64(d)
	}
	return 0
}

//export tinygo_runtime_bdwgc_set_pace
func tinygo_runtime_bdwgc_set_pace(trigger uintptr)
