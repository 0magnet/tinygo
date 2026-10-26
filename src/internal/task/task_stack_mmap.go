//go:build scheduler.tasks && linux && !baremetal

package task

import "unsafe"

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

func flushUnmap() {
	if unmapNext != 0 {
		stackMunmap(unsafe.Pointer(unmapNext-stackGuard), unmapNextSize+stackGuard)
		unmapNext = 0
	}
}

func allocStack(s *state, size uintptr) unsafe.Pointer {
	flushUnmap()
	s.stack.size = size
	for p := &pool; *p != nil; p = &(*p).next {
		if ps := *p; ps.size == size {
			*p = ps.next
			poolCount--
			s.stack.top = uintptr(unsafe.Pointer(ps)) + size
			return unsafe.Pointer(ps)
		}
	}
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
	t.state.stack.next = stackTasks
	if stackTasks != nil {
		stackTasks.state.stack.prev = t
	}
	stackTasks = t
}

// unlinkStack releases the stack of the exiting task, which is still running
// on it. Nothing else runs on this thread before the task pauses for good.
func unlinkStack(t *Task) {
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

// MappedStackTop returns the end of the current goroutine's stack, or zero on the
// system stack.
func MappedStackTop() uintptr {
	if currentTask == nil {
		return 0
	}
	return currentTask.state.stack.top
}

// MarkParkedStacks passes the used part of every parked goroutine stack to mark.
// The running goroutine's stack is scanned with its registers instead.
func MarkParkedStacks(mark func(start, end uintptr)) {
	for t := stackTasks; t != nil; t = t.state.stack.next {
		if t != currentTask {
			mark(t.state.sp, t.state.stack.top)
		}
	}
}

// GoroutineStacks calls fn with the stack of every parked goroutine, innermost
// first, and reports true. The slice is reused between calls.
func GoroutineStacks(fn func(pcs []uintptr)) bool {
	var buf [64]uintptr
	for t := stackTasks; t != nil; t = t.state.stack.next {
		if t != currentTask {
			fn(buf[:walkStack(t.state.sp, t.state.stack.top, buf[:])])
		}
	}
	return true
}
