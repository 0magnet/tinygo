//go:build (scheduler.tasks || scheduler.cores) && linux && !baremetal

package task

import (
	"runtime/volatile"
	"sync/atomic"
	"unsafe"
)

// Goroutine stacks are mapped outside the GC heap, so untouched pages cost no
// memory and the GC scans only the part of each stack in use. A guard page
// below each stack turns an overflow into a fault.

const hasReleasableStack = true

const (
	stackGuard = 64 * 1024 // a whole page on 4K, 16K and 64K page kernels

	protNone  = 0
	protRW    = 1 | 2
	mapFlags  = 0x02 | 0x20 | 0x4000 | 0x20000 // private, anonymous, noreserve, stack
	maxPooled = 1024
)

type stackInfo struct {
	top        uintptr // end of the stack
	size       uintptr // usable size, above the guard
	prev, next *Task   // tasks with a mapped stack
}

var (
	stackTasks *Task // every task with a mapped stack

	// stackLock guards the task list, the pool and unmapNext. Nothing
	// allocates while holding it, so the GC can take it.
	stackLock atomic.Uint32

	// Released stacks for reuse, linked through a header at their bases. A
	// header at the top would overwrite the exiting task's first frames.
	pool      *pooledStack
	poolCount int

	// A released stack that could not be pooled. It is unmapped by the next
	// release or allocation, which never runs on it.
	unmapNext, unmapNextSize uintptr
)

type pooledStack struct {
	next *pooledStack
	size uintptr
}

// The guard page catches an overflow, so the canary is a static word that
// keeps the bottom page of each stack untouched.
var mappedCanary uintptr

func canaryFor(stack unsafe.Pointer) *uintptr { return &mappedCanary }

//export mmap
func stackMmap(addr unsafe.Pointer, length uintptr, prot, flags, fd int32, offset uintptr) unsafe.Pointer

//export munmap
func stackMunmap(addr unsafe.Pointer, length uintptr) int32

//export mprotect
func stackMprotect(addr unsafe.Pointer, len uintptr, prot int32) int32

// LockStacks takes the lock on the stack list. The GC holds it while it scans
// stacks with the world stopped.
func LockStacks() {
	for !stackLock.CompareAndSwap(0, 1) {
		spinWait()
	}
}

// UnlockStacks releases the lock taken by LockStacks.
func UnlockStacks() {
	stackLock.Store(0)
}

func flushUnmap() {
	if unmapNext != 0 {
		stackMunmap(unsafe.Pointer(unmapNext-stackGuard), unmapNextSize+stackGuard)
		unmapNext = 0
	}
}

func allocStack(s *state, size uintptr) unsafe.Pointer {
	LockStacks()
	flushUnmap()
	s.stack.size = size
	for p := &pool; *p != nil; p = &(*p).next {
		if ps := *p; ps.size == size {
			*p = ps.next
			poolCount--
			UnlockStacks()
			clearReusedStack(uintptr(unsafe.Pointer(ps)), size)
			s.stack.top = uintptr(unsafe.Pointer(ps)) + size
			return unsafe.Pointer(ps)
		}
	}
	UnlockStacks()
	m := stackMmap(nil, size+stackGuard, protRW, mapFlags, -1, 0)
	if uintptr(m) == ^uintptr(0) {
		runtimeFatal("cannot map goroutine stack")
	}
	stackMprotect(m, stackGuard, protNone)
	base := uintptr(m) + stackGuard
	s.stack.top = base + size
	return unsafe.Pointer(base)
}

func linkTask(t *Task) {
	LockStacks()
	t.state.stack.next = stackTasks
	if stackTasks != nil {
		stackTasks.state.stack.prev = t
	}
	stackTasks = t
	UnlockStacks()
}

// unlinkStack removes the exiting task, which still runs on its stack, from
// the list. Its stack is reused once nothing runs on it any more.
func unlinkStack(t *Task) {
	LockStacks()
	si := &t.state.stack
	if si.prev != nil {
		si.prev.state.stack.next = si.next
	} else {
		stackTasks = si.next
	}
	if si.next != nil {
		si.next.state.stack.prev = si.prev
	}
	si.prev, si.next = nil, nil
	if !stackReleasedOnResume {
		// Nothing else runs on this thread before the task pauses for good.
		releaseStackLocked(si)
	}
	UnlockStacks()
}

// releaseStack pools or unmaps the stack of an exited task.
func releaseStack(t *Task) {
	LockStacks()
	releaseStackLocked(&t.state.stack)
	UnlockStacks()
}

func releaseStackLocked(si *stackInfo) {
	flushUnmap()
	if poolCount < maxPooled {
		ps := (*pooledStack)(unsafe.Pointer(si.top - si.size))
		ps.next, ps.size = pool, si.size
		pool = ps
		poolCount++
	} else {
		unmapNext, unmapNextSize = si.top-si.size, si.size
	}
}

//export madvise
func stackMadvise(addr unsafe.Pointer, length uintptr, advice int32) int32

//export getpagesize
func stackPageSize() int32

const madvDontNeed = 4

// ReleaseIdleStackPages gives back the pages of each parked stack below where
// it was parked, and of each pooled stack but its header, as Go shrinks stacks
// in the GC. The world must be stopped. onCore reports goroutines on a core.
func ReleaseIdleStackPages(onCore func(*Task) bool) {
	page := uintptr(stackPageSize())
	for t := stackTasks; t != nil; t = t.state.stack.next {
		if t.RunState != RunStatePaused || onCore(t) {
			continue
		}
		low := t.state.stack.top - t.state.stack.size
		if end := (t.state.sp - 128) &^ (page - 1); end > low {
			stackMadvise(unsafe.Pointer(low), end-low, madvDontNeed)
		}
	}
	for ps := pool; ps != nil; ps = ps.next {
		if base := uintptr(unsafe.Pointer(ps)); ps.size > page {
			stackMadvise(unsafe.Pointer(base+page), ps.size-page, madvDontNeed)
		}
	}
}

// MappedStackTop returns the end of the current goroutine's stack, or zero on the
// system stack.
func MappedStackTop() uintptr {
	t := Current()
	if t == nil {
		return 0
	}
	return t.state.stack.top
}

// MappedStackOf returns the bounds of t's stack.
func MappedStackOf(t *Task) (low, top uintptr) {
	return t.state.stack.top - t.state.stack.size, t.state.stack.top
}

// MarkParkedStacks passes the used part of every goroutine stack but the
// current one to mark, from where each was last parked. A goroutine running
// on another core is scanned by that core as well. The caller holds
// LockStacks when other cores run.
func MarkParkedStacks(mark func(start, end uintptr)) {
	current := Current()
	for t := stackTasks; t != nil; t = t.state.stack.next {
		if t != current {
			mark(t.state.sp, t.state.stack.top)
		}
	}
}

// GoroutineStacks calls fn with why each goroutine waits and its stack,
// innermost first, and reports true. A goroutine running on a core, the
// caller among them, has no stack. The slice is reused between calls.
func GoroutineStacks(fn func(reason WaitReason, pcs []uintptr)) bool {
	if !stackReleasedOnResume {
		var buf [64]uintptr
		current := Current()
		fn(WaitRunning, nil)
		for t := stackTasks; t != nil; t = t.state.stack.next {
			if t != current {
				fn(t.WaitReason, buf[:walkStack(t.state.sp, t.state.stack.top, buf[:])])
			}
		}
		return true
	}

	// Other cores start and end goroutines meanwhile, and fn allocates, so
	// copy the stacks out under the lock and call fn after. The lock keeps
	// every stack mapped, so a walk reads only memory of the stack it walks.
	const depth = 32
	LockStacks()
	n := 0
	for t := stackTasks; t != nil; t = t.state.stack.next {
		n++
	}
	UnlockStacks()
	n += n/8 + 16
	pcs := make([]uintptr, n*depth)
	lens := make([]uint8, n)
	reasons := make([]WaitReason, n)
	LockStacks()
	i := 0
	for t := stackTasks; t != nil && i < n; t = t.state.stack.next {
		reasons[i], lens[i] = WaitRunning, 0
		if sp := parkedSP(t); volatile.LoadUint8(&t.RunState) == RunStatePaused {
			r := t.WaitReason
			l := walkStack(sp, t.state.stack.top, pcs[i*depth:(i+1)*depth])
			// A goroutine another core resumed during the walk may have
			// changed its frames, so it counts as running.
			if volatile.LoadUint8(&t.RunState) == RunStatePaused && parkedSP(t) == sp {
				reasons[i], lens[i] = r, uint8(l)
			}
		}
		i++
	}
	UnlockStacks()
	for j := 0; j < i; j++ {
		fn(reasons[j], pcs[j*depth:j*depth+int(lens[j])])
	}
	return true
}

// reusedStackClear is how much of the top of a pooled stack is cleared when a
// new goroutine takes it, where its frames will be.
const reusedStackClear = 64 * 1024

// clearReusedStack clears the top of a pooled stack. The GC scans a parked
// stack conservatively from its sp, so words the exited goroutine left in
// slots the new one never writes would keep what they point at alive.
func clearReusedStack(base, size uintptr) {
	n := uintptr(reusedStackClear)
	if limit := size - unsafe.Sizeof(pooledStack{}); n > limit {
		n = limit
	}
	memzero(unsafe.Pointer(base+size-n), n)
}

//go:linkname memzero runtime.memzero
func memzero(ptr unsafe.Pointer, size uintptr)

// parkedSP reads where t was last parked, which another core may change.
func parkedSP(t *Task) uintptr {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return uintptr(volatile.LoadUint64((*uint64)(unsafe.Pointer(&t.state.sp))))
	}
	return uintptr(volatile.LoadUint32((*uint32)(unsafe.Pointer(&t.state.sp))))
}
