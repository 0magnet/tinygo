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
)

func gcAllocLock()   {}
func gcAllocUnlock() {}

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
		d := tinygo_runtime_bdwgc_clock(0) - gcStopStart
		gcPauseTotal += d
		gcLastGC = tinygo_runtime_bdwgc_clock(1)
		if gcTrace {
			println("gc pause", d/1000, "us")
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

//export GC_allow_register_threads
func libgc_allow_register_threads()

//export GC_register_my_thread
func libgc_register_my_thread(*libgc_stack_base) int32

//export tinygo_runtime_bdwgc_threads_init
func tinygo_runtime_bdwgc_threads_init() int32

//export tinygo_runtime_bdwgc_clock
func tinygo_runtime_bdwgc_clock(realtime int32) uint64

//export tinygo_runtime_bdwgc_chain_suspend
func tinygo_runtime_bdwgc_chain_suspend(thread *threadID)
