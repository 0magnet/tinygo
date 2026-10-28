//go:build gc.boehm

// This is the Boehm-Demers-Weiser conservative garbage collector, integrated
// into TinyGo.
//
// Without bdwgc thread support, threads are handled this way:
//   * All calls to the bdwgc library are serialized using locks.
//   * When the bdwgc library wants to push GC roots, all other threads that are
//     running are stopped.
//   * After returning from a bdwgc library call, the caller checks whether
//     other threads were stopped (meaning a GC cycle happened) and resumes the
//     world.
// The cores scheduler on Linux uses bdwgc thread support instead, see
// gc_boehm_threads.go.

package runtime

import (
	"internal/gclayout"
	"internal/task"
	"unsafe"
)

const needsStaticHeap = false

const boehmLayoutSizeBits = 4 + unsafe.Sizeof(uintptr(0))/4
const (
	boehmPtrFreeKind = 0
	boehmNormalKind  = 1
)

// gcLock serializes the runtime state kept next to bdwgc, and every bdwgc call
// unless bdwgc has its own thread support.
var gcLock task.PMutex

func initHeap() {
	libgc_init()
	metricsStart = nanotime()

	// Call GC_set_push_other_roots(gcCallback) in C because of function
	// signature differences that do matter in WebAssembly.
	gcInit()
	gcInitThreads()
}

//export tinygo_runtime_bdwgc_init
func gcInit()

//export tinygo_runtime_bdwgc_callback
func gcCallback() {
	// Mark globals and all stacks, and stop the world if we're using threading.
	gcMarkReachable()
}

func markRoots(start, end uintptr) {
	libgc_push_all(start, end)
}

func markCurrentGoroutineStack(sp uintptr) {
	if top := task.MappedStackTop(); top != 0 {
		libgc_push_all_stack(sp, top) // a stack mapped outside the heap
		return
	}
	// Only mark the area of the stack that is currently in use.
	// (This doesn't work for other goroutines, but at least it doesn't keep
	// more pointers alive than needed on the current stack).
	base := libgc_base(sp)
	if base == 0 { // && asserts
		runtimeFatal("goroutine stack not in a heap allocation?")
	}
	stackBottom := base + libgc_size(base)
	libgc_push_all_stack(sp, stackBottom)
}

//go:noinline
func alloc(size uintptr, layout unsafe.Pointer) unsafe.Pointer {
	if size == 0 {
		return alloc_zero(size, layout)
	}

	gcAllocLock()
	gcCountMalloc()
	var ptr unsafe.Pointer
	var needsZero bool
	for attempt := 0; ; attempt++ {
		ptr, needsZero = boehmMalloc(size, layout)
		if ptr != nil || !gcOutOfMemory(attempt) {
			break
		}
	}
	if ptr != nil {
		gcProfileAlloc(ptr, size, uintptr(returnAddress(0)))
	}
	gcAllocUnlock()
	if ptr == nil {
		runtimeFatal("gc: out of memory")
		return nil
	}
	if needsZero {
		memzero(ptr, size)
	}
	return ptr
}

// boehmMalloc allocates size bytes of the bdwgc kind for layout. It returns nil
// when bdwgc is out of memory.
func boehmMalloc(size uintptr, layout unsafe.Pointer) (ptr unsafe.Pointer, needsZero bool) {
	switch layout {
	case gclayout.NoPtrs.AsPtr():
		// This object is entirely pointer free, for example make([]int, ...).
		// Make sure the GC knows this so it doesn't scan the object
		// unnecessarily to improve performance.
		ptr = libgc_malloc_kind(size, boehmPtrFreeKind)
		needsZero = true
	case gclayout.Conservative.AsPtr():
		// Stack storage does not have an ordinary repeating Go object layout.
		ptr = libgc_malloc_kind(size, boehmNormalKind)
	case gclayout.Pointer.AsPtr(), gclayout.PointerPair.AsPtr():
		// Conservative scanning is exact when every word is a pointer.
		ptr = libgc_malloc_kind(size, boehmNormalKind)
	default:
		elementWords := boehmLayoutElementWords(layout)
		pointerAlign := unsafe.Alignof(uintptr(0))
		if elementWords == 0 || elementWords > size/pointerAlign {
			// This should not happen for compiler-generated Go allocations.
			ptr = libgc_malloc_kind(size, boehmNormalKind)
			break
		}
		elementSize := elementWords * pointerAlign
		if size%elementSize != 0 {
			ptr = libgc_malloc_kind(size, boehmNormalKind)
			break
		}

		descriptor := libgc_make_descriptor(uintptr(layout))
		if descriptor == 0 {
			// The bridge returns zero if its cache or bitmap allocation fails.
			// It also rejects the no-pointer descriptor, which is handled above.
			ptr = libgc_malloc_kind(size, boehmNormalKind)
			break
		}
		elementCount := size / elementSize
		if elementCount == 1 {
			ptr = libgc_malloc_explicitly_typed(size, descriptor)
		} else {
			ptr = libgc_calloc_explicitly_typed(
				elementCount, elementSize, descriptor,
			)
		}
	}
	return
}

func boehmLayoutElementWords(layout unsafe.Pointer) uintptr {
	value := uintptr(layout)
	if value&1 != 0 {
		return (value >> 1) & (1<<boehmLayoutSizeBits - 1)
	}
	return *(*uintptr)(layout)
}

func allocManual(size uintptr) unsafe.Pointer {
	if size == 0 {
		return alloc_zero(size, gclayout.NoPtrs.AsPtr())
	}

	gcLock.Lock()
	ptr := libgc_malloc_atomic_uncollectable(size)
	gcResumeWorld()
	gcLock.Unlock()
	if ptr == nil {
		runtimeFatal("gc: out of memory")
		return nil
	}
	memzero(ptr, size)
	return ptr
}

func free(ptr unsafe.Pointer) {
	gcLock.Lock()
	libgc_free(ptr)
	gcResumeWorld()
	gcLock.Unlock()
}

//go:noinline
func freeTaskStack(ptr uintptr) {
	free(unsafe.Pointer(ptr))
}

func GC() {
	gcLock.Lock()
	libgc_gcollect()
	gcResumeWorld()
	gcLock.Unlock()
}

// This should be stack-allocated, but we don't currently have a good way of
// ensuring that happens.
var gcMemStats libgc_prof_stats

func ReadMemStats(m *MemStats) {
	gcLock.Lock()

	libgc_get_prof_stats(&gcMemStats, unsafe.Sizeof(gcMemStats))

	// The heap splits as in runtime/metrics. bdwgc does not keep objects
	// apart from its free lists, so HeapInuse is HeapAlloc.
	objects, free, released, total := boehmHeapClasses(&gcMemStats)
	m.HeapAlloc = objects
	m.HeapInuse = objects
	m.HeapIdle = free + released
	m.HeapReleased = released
	m.HeapSys = objects + free + released
	m.Alloc = m.HeapAlloc
	m.GCSys = 0 // not provided by bdwgc
	m.TotalAlloc = uint64(gcMemStats.allocd_bytes_before_gc + gcMemStats.bytes_allocd_since_gc)
	m.Mallocs = gcMallocCount()
	m.Frees = 0 // not provided by bdwgc
	m.Sys = total
	live, trigger := gcMarked()
	m.NextGC = boehmHeapGoal(live, trigger)
	m.GCCPUFraction = gcCPUFraction()
	m.NumGC = uint32(gcMemStats.gc_no)
	gcReadPauses(m)

	gcLock.Unlock()
}

func mallocs() uint64 {
	gcLock.Lock()
	mallocs := gcMallocCount()
	gcLock.Unlock()
	return mallocs
}

func setHeapEnd(newHeapEnd uintptr) {
	runtimeFatal("gc: did not expect setHeapEnd call")
}

func SetFinalizer(obj interface{}, finalizer interface{}) {
	// Unimplemented.
	// The GC *does* support finalization, so this could be added relatively
	// easily I think.
}

//export GC_init
func libgc_init()

//export GC_malloc_kind
func libgc_malloc_kind(uintptr, int32) unsafe.Pointer

//export GC_malloc_atomic_uncollectable
func libgc_malloc_atomic_uncollectable(uintptr) unsafe.Pointer

//export tinygo_runtime_bdwgc_make_descriptor
func libgc_make_descriptor(uintptr) uintptr

//export GC_calloc_explicitly_typed
func libgc_calloc_explicitly_typed(uintptr, uintptr, uintptr) unsafe.Pointer

//export GC_malloc_explicitly_typed
func libgc_malloc_explicitly_typed(uintptr, uintptr) unsafe.Pointer

//export GC_free
func libgc_free(unsafe.Pointer)

//export GC_base
func libgc_base(ptr uintptr) uintptr

//export GC_size
func libgc_size(ptr uintptr) uintptr

//export GC_push_all
func libgc_push_all(bottom, top uintptr)

//export GC_push_all_eager
func libgc_push_all_eager(bottom, top uintptr)

//export GC_push_all_stack
func libgc_push_all_stack(bottom, top uintptr)

//export GC_gcollect
func libgc_gcollect()

//export GC_get_prof_stats
func libgc_get_prof_stats(*libgc_prof_stats, uintptr) uintptr

//export GC_set_push_other_roots
func libgc_set_push_other_roots(unsafe.Pointer)

type libgc_prof_stats struct {
	heapsize_full             uintptr
	free_bytes_full           uintptr
	unmapped_bytes            uintptr
	bytes_allocd_since_gc     uintptr
	allocd_bytes_before_gc    uintptr
	non_gc_bytes              uintptr
	gc_no                     uintptr
	markers_m1                uintptr
	bytes_reclaimed_since_gc  uintptr
	reclaimed_bytes_before_gc uintptr
	expl_freed_bytes_since_gc uintptr
	obtained_from_os_bytes    uintptr
}
