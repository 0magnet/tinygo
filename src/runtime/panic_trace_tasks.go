//go:build linux && !baremetal && !nintendoswitch && !wasip1 && !wasm_unknown && !wasip2 && (scheduler.tasks || scheduler.cores)

package runtime

import (
	"internal/task"
	"unsafe"
)

// Enough to name the frames of tens of thousands of goroutines in one pass.
const allTraceAddrs = 1 << 16

var (
	allTraceBuf [panicTraceDepth]uintptr
	allSyms     symTable
)

func taskStackBounds() (lo, hi uintptr) {
	t := task.Current()
	if t == nil {
		return 0, 0
	}
	return task.MappedStackOf(t)
}

// printOtherGoroutines prints the stack of every goroutine but self, as Go
// does with GOTRACEBACK=all. It walks them once to name all their frames in
// one pass over the symbol table, and again to print them.
func printOtherGoroutines(self *task.Task) {
	if !task.TryLockStacks(1 << 20) {
		crashStr("\nother goroutines not shown: the stack list stayed locked\n")
		return
	}
	syms := &allSyms
	*syms = symTable{}
	const size = allTraceAddrs * (2*unsafe.Sizeof(uintptr(0)) + 4)
	if m := mmap(nil, size, 3, 0x22, -1, 0); uintptr(m) != ^uintptr(0) {
		syms.addrs = unsafe.Slice((*uintptr)(m), allTraceAddrs)[:0]
		syms.starts = unsafe.Slice((*uintptr)(unsafe.Add(m, allTraceAddrs*unsafe.Sizeof(uintptr(0)))), allTraceAddrs)
		syms.names = unsafe.Slice((*uint32)(unsafe.Add(m, 2*allTraceAddrs*unsafe.Sizeof(uintptr(0)))), allTraceAddrs)
	}
	full := syms.addrs == nil
	for t := task.NextStackTask(nil); t != nil && !full; t = task.NextStackTask(t) {
		if t == self {
			continue
		}
		_, n, _ := task.ParkedStack(t, allTraceBuf[:])
		for i := 0; i < n && !full; i++ {
			if !syms.add(allTraceBuf[i] - 1) {
				syms.compact()
				full = !syms.add(allTraceBuf[i] - 1)
			}
		}
	}
	if syms.addrs != nil {
		syms.resolve()
	}
	for t := task.NextStackTask(nil); t != nil; t = task.NextStackTask(t) {
		if t == self {
			continue
		}
		reason, n, parked := task.ParkedStack(t, allTraceBuf[:])
		crashStr("\ngoroutine ")
		crashHex(uintptr(unsafe.Pointer(t)))
		crashStr(" [")
		crashStr(reason.String())
		crashStr("]:\n")
		if !parked {
			crashStr("\tgoroutine running on another core; stack unavailable\n")
		}
		for i := 0; i < n; i++ {
			printFrame(syms, allTraceBuf[i], false)
		}
	}
	task.UnlockStacks()
}
