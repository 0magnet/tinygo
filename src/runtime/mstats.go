package runtime

// Memory statistics

// Subset of memory statistics from upstream Go.
// Works with conservative gc only.

// A MemStats records statistics about the memory allocator.
type MemStats struct {
	// General statistics.

	// Alloc is bytes of allocated heap objects.
	//
	// This is the same as HeapAlloc (see below).
	Alloc uint64

	// Sys is the total bytes of memory obtained from the OS.
	//
	// Sys is the sum of the XSys fields below. Sys measures the
	// address space reserved by the runtime for the
	// heap, stacks, and other internal data structures.
	Sys uint64

	// Heap memory statistics.

	// HeapAlloc is bytes of allocated heap objects.
	//
	// "Allocated" heap objects include all reachable objects, as
	// well as unreachable objects that the garbage collector has
	// not yet freed. Specifically, HeapAlloc increases as heap
	// objects are allocated and decreases as the heap is swept
	// and unreachable objects are freed. Sweeping occurs
	// incrementally between GC cycles, so these two processes
	// occur simultaneously, and as a result HeapAlloc tends to
	// change smoothly (in contrast with the sawtooth that is
	// typical of stop-the-world garbage collectors).
	HeapAlloc uint64

	// HeapSys is bytes of heap memory, total.
	//
	// In TinyGo unlike upstream Go, we make no distinction between
	// regular heap blocks used by escaped-to-the-heap variables and
	// blocks occupied by goroutine stacks,
	// all such blocks are marked as in-use, see HeapInuse below.
	HeapSys uint64

	// HeapIdle is bytes in idle (unused) blocks.
	HeapIdle uint64

	// HeapInuse is bytes in in-use blocks.
	HeapInuse uint64

	// HeapReleased is bytes of physical memory returned to the OS.
	HeapReleased uint64

	// HeapObjects is the number of allocated heap objects.
	//
	// Like HeapAlloc, this increases as objects are allocated and
	// decreases as the heap is swept and unreachable objects are
	// freed.
	HeapObjects uint64

	// TotalAlloc is cumulative bytes allocated for heap objects.
	//
	// TotalAlloc increases as heap objects are allocated, but
	// unlike Alloc and HeapAlloc, it does not decrease when
	// objects are freed.
	TotalAlloc uint64

	// Mallocs is the cumulative count of heap objects allocated.
	// The number of live objects is Mallocs - Frees.
	Mallocs uint64

	// Frees is the cumulative count of heap objects freed.
	Frees uint64

	// Off-heap memory statistics.
	//
	// The following statistics measure runtime-internal
	// structures that are not allocated from heap memory (usually
	// because they are part of implementing the heap).

	// GCSys is bytes of memory in garbage collection metadata.
	GCSys uint64

	// NumGC is the number of completed GC cycles.
	//
	// The leaking collector never collects, so it always reports 0.
	NumGC uint32

	// The fields below exist so code written against upstream Go compiles,
	// prometheus/client_golang among it. TinyGo never fills them in.

	// Lookups is the number of pointer lookups performed by the runtime.
	Lookups uint64
	// StackInuse is bytes in stack spans.
	StackInuse uint64
	// StackSys is bytes of stack memory obtained from the OS.
	StackSys uint64
	// MSpanInuse is bytes of allocated mspan structures.
	MSpanInuse uint64
	// MSpanSys is bytes of memory obtained from the OS for mspan structures.
	MSpanSys uint64
	// MCacheInuse is bytes of allocated mcache structures.
	MCacheInuse uint64
	// MCacheSys is bytes of memory obtained from the OS for mcache structures.
	MCacheSys uint64
	// BuckHashSys is bytes of memory in profiling bucket hash tables.
	BuckHashSys uint64
	// OtherSys is bytes of memory in miscellaneous off-heap runtime allocations.
	OtherSys uint64
	// NextGC is the target heap size of the next GC cycle.
	NextGC uint64
	// LastGC is the time the last garbage collection finished, in nanoseconds since 1970.
	LastGC uint64
	// PauseTotalNs is the cumulative nanoseconds in GC stop-the-world pauses.
	PauseTotalNs uint64
	// NumForcedGC is the number of GC cycles forced by the application.
	NumForcedGC uint32
	// GCCPUFraction is the fraction of this program's available CPU time used by the GC.
	GCCPUFraction float64
	// EnableGC reports that GC is enabled.
	EnableGC bool
	// DebugGC is unused.
	DebugGC bool
}
