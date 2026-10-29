//go:build linux && !baremetal && !nintendoswitch && !wasip1 && !wasm_unknown && !wasip2

package runtime

// A fatal panic prints the stack of the panicking goroutine to stderr, walked
// through frame pointers and named from the ELF symbol table, so it works in
// -no-debug builds. The stack is recorded when the panic starts, before its
// deferred calls unwind it, and the record is dropped if the panic is
// recovered. Nothing here allocates: a fault can happen with the GC lock held.

import (
	"internal/task"
	"sync/atomic"
	"unsafe"
)

const panicTraceDepth = 64

type panicTrace struct {
	owner uintptr // the panicking task, or zero for a free record
	sig   uintptr // the signal that raised the panic, or zero
	code  uintptr // its si_code
	fault uintptr // its si_addr
	pc    uintptr // the faulting pc, or the return address into the panicking function
	n     uintptr
	pcs   [panicTraceDepth + 8]uintptr // a little spare for frames the trim drops
}

var (
	// Records of panics that are unwinding, one per panicking goroutine.
	panicTraces [16]panicTrace
	// The record of a fatal error, which never unwinds.
	fatalTrace panicTrace

	crashTraceOwner uintptr
	crashBuf        [512]byte
	crashLen        int
	symName         [256]byte

	// The addresses a trace looks up, ascending, and what they resolve to.
	symAddrBuf  [2*panicTraceDepth + 16]uintptr
	symStartBuf [2*panicTraceDepth + 16]uintptr
	symNameBuf  [2*panicTraceDepth + 16]uint32
	traceSyms   symTable
)

//export tinygo_crash_walk
func tinygo_crash_walk(pcs *uintptr, n, fp, lo, hi uintptr) uintptr

//export tinygo_crash_symbolize
func tinygo_crash_symbolize(addrs *uintptr, n uintptr, starts *uintptr, names *uint32) int32

//export tinygo_crash_symname
func tinygo_crash_symname(off uint32, buf *byte, n uintptr) uintptr

//export tinygo_crash_all
func tinygo_crash_all() int32

//export tinygo_thread_stack_lo
func tinygo_thread_stack_lo() uintptr

//export tinygo_thread_stack_hi
func tinygo_thread_stack_hi() uintptr

// capturePanicTrace records the stack of the goroutine whose panic starts at
// pc. A later panic of the same goroutine, in a deferred call, replaces it.
//
//go:noinline
func capturePanicTrace(pc, sig, code, fault uintptr) {
	t := task.Current()
	if t == nil {
		return
	}
	id := uintptr(unsafe.Pointer(t))
	r := ownPanicTrace(id)
	for i := 0; r == nil && i < len(panicTraces); i++ {
		if atomic.CompareAndSwapUintptr(&panicTraces[i].owner, 0, id) {
			r = &panicTraces[i]
		}
	}
	if r == nil {
		return
	}
	fillTrace(r, pc, 0)
	r.sig, r.code, r.fault = sig, code, fault
}

// releasePanicTrace drops the record of a panic that was recovered.
func releasePanicTrace() {
	if t := task.Current(); t != nil {
		if r := ownPanicTrace(uintptr(unsafe.Pointer(t))); r != nil {
			atomic.StoreUintptr(&r.owner, 0)
		}
	}
}

func ownPanicTrace(id uintptr) *panicTrace {
	for i := range panicTraces {
		if atomic.LoadUintptr(&panicTraces[i].owner) == id {
			return &panicTraces[i]
		}
	}
	return nil
}

// fillTrace walks the current stack into r, from fp or from here if fp is
// zero, and drops the frames above pc. Without pc it drops the frames of the
// trace code itself.
//
//go:noinline
func fillTrace(r *panicTrace, pc, fp uintptr) {
	at := fp
	if at == 0 {
		at = uintptr(stacksave())
	}
	lo, hi := crashStackBounds(at)
	n := tinygo_crash_walk(&r.pcs[0], uintptr(len(r.pcs)), fp, lo, hi)
	start := uintptr(0)
	if fp == 0 {
		start = 2
		for i := uintptr(0); pc != 0 && i < n; i++ {
			if r.pcs[i] == pc {
				start = i
				break
			}
		}
	}
	if start > n {
		start = n
	}
	copy(r.pcs[:], r.pcs[start:n])
	r.n = n - start
	if r.n > panicTraceDepth {
		r.n = panicTraceDepth
	}
	r.pc = pc
	r.sig, r.code, r.fault = 0, 0, 0
}

// crashStackBounds returns the bounds of the stack that holds addr: the
// goroutine stack where the scheduler maps it, or the thread stack.
func crashStackBounds(addr uintptr) (lo, hi uintptr) {
	if lo, hi = taskStackBounds(); lo != 0 && lo <= addr && addr < hi {
		return lo, hi
	}
	if lo, hi = tinygo_thread_stack_lo(), tinygo_thread_stack_hi(); lo <= addr && addr < hi {
		return lo, hi
	}
	return 0, 0
}

// printPanicTrace prints the stack recorded when the current goroutine's
// panic started, or the current stack if none was recorded.
func printPanicTrace() {
	if !lockCrashTrace() {
		return
	}
	r := (*panicTrace)(nil)
	if t := task.Current(); t != nil {
		r = ownPanicTrace(uintptr(unsafe.Pointer(t)))
	}
	if r == nil {
		r = &fatalTrace
		fillTrace(r, 0, 0)
	}
	printTrace(r)
}

// printFatalTrace prints the current stack for a fatal error.
//
//go:noinline
func printFatalTrace() {
	if !lockCrashTrace() {
		return
	}
	fillTrace(&fatalTrace, 0, 0)
	printTrace(&fatalTrace)
}

// printSignalTrace prints the stack of a signal the handler could not turn
// into a panic, from the pc and frame pointer where it hit. The handler runs
// on the signal stack, so the walk starts from the signal context.
func printSignalTrace(sig, code, fault, pc, fp uintptr) {
	if !lockCrashTrace() {
		return
	}
	r := &fatalTrace
	r.pcs[0] = pc
	lo, hi := crashStackBounds(fp)
	n := tinygo_crash_walk(&r.pcs[1], panicTraceDepth-1, fp, lo, hi)
	r.n = n + 1
	r.pc, r.sig, r.code, r.fault = pc, sig, code, fault
	printTrace(r)
}

// lockCrashTrace lets one goroutine print a trace. A crash elsewhere waits
// for the first one to end the process, and a crash inside the trace code
// itself gives up so the process still aborts.
func lockCrashTrace() bool {
	id := uintptr(unsafe.Pointer(task.Current()))
	if id == 0 {
		id = tinygo_thread_stack_hi() | 1
	}
	for {
		if atomic.CompareAndSwapUintptr(&crashTraceOwner, 0, id) {
			return true
		}
		if atomic.LoadUintptr(&crashTraceOwner) == id {
			return false
		}
		usleep(1000)
	}
}

func printTrace(r *panicTrace) {
	// The fault pc was not pushed by a call, so it is looked up as is. A
	// return address can be the first byte of the next function.
	exact := r.sig != 0 && r.n > 0 && r.pcs[0] == r.pc
	syms := &traceSyms
	*syms = symTable{addrs: symAddrBuf[:0], starts: symStartBuf[:], names: symNameBuf[:]}
	if r.sig != 0 {
		syms.add(r.pc)
	}
	for i := uintptr(0); i < r.n; i++ {
		syms.add(lookupAddr(r.pcs[i], i == 0 && exact))
	}
	syms.resolve()

	crashByte('\n')
	if r.sig != 0 {
		crashStr("[signal ")
		crashStr(signalName(r.sig))
		crashStr(" code=")
		crashHex(r.code)
		crashStr(" addr=")
		crashHex(r.fault)
		crashStr(" pc=")
		crashHex(r.pc)
		crashByte(' ')
		syms.print(r.pc, r.pc)
		crashStr("]\n\n")
	}
	crashStr("goroutine ")
	crashHex(uintptr(unsafe.Pointer(task.Current())))
	crashStr(" [running]:\n")
	if r.n == 0 {
		crashStr("\tno frames: the stack bounds are unknown or the frame chain is broken\n")
	}
	for i := uintptr(0); i < r.n; i++ {
		printFrame(syms, r.pcs[i], i == 0 && exact)
	}
	if r.n == panicTraceDepth {
		crashStr("...additional frames elided...\n")
	}
	crashFlush()
	if tinygo_crash_all() != 0 {
		printOtherGoroutines(task.Current())
		crashFlush()
	}
}

func lookupAddr(pc uintptr, exact bool) uintptr {
	if exact {
		return pc
	}
	return pc - 1
}

func printFrame(syms *symTable, pc uintptr, exact bool) {
	syms.print(lookupAddr(pc, exact), pc)
	crashStr("\n\tpc=")
	crashHex(pc)
	crashByte('\n')
}

func signalName(sig uintptr) string {
	switch sig {
	case sig_SIGSEGV:
		return "SIGSEGV: segmentation violation"
	case sig_SIGBUS:
		return "SIGBUS: bus error"
	case sig_SIGFPE:
		return "SIGFPE: floating-point exception"
	case sig_SIGILL:
		return "SIGILL: illegal instruction"
	}
	return "unknown signal"
}

// symTable names a set of addresses with one pass over the symbol table.
type symTable struct {
	addrs  []uintptr // ascending and unique once resolved
	starts []uintptr
	names  []uint32
	ok     bool
}

func (s *symTable) add(addr uintptr) bool {
	if len(s.addrs) == cap(s.addrs) {
		return false
	}
	s.addrs = append(s.addrs, addr)
	return true
}

// compact sorts the addresses and drops duplicates.
func (s *symTable) compact() {
	a := s.addrs
	heapSortUintptr(a)
	n := 0
	for i := range a {
		if n == 0 || a[i] != a[n-1] {
			a[n] = a[i]
			n++
		}
	}
	s.addrs = a[:n]
}

func (s *symTable) resolve() {
	s.compact()
	if len(s.addrs) > 0 {
		s.ok = tinygo_crash_symbolize(&s.addrs[0], uintptr(len(s.addrs)), &s.starts[0], &s.names[0]) != 0
	}
}

// print writes the function holding addr and the offset of pc into it.
func (s *symTable) print(addr, pc uintptr) {
	lo, hi := 0, len(s.addrs)
	for lo < hi {
		m := lo + (hi-lo)/2
		if s.addrs[m] < addr {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if !s.ok || lo == len(s.addrs) || s.addrs[lo] != addr || s.starts[lo] == 0 {
		crashByte('?')
		return
	}
	n := tinygo_crash_symname(s.names[lo], &symName[0], uintptr(len(symName)))
	if n == 0 {
		crashByte('?')
	}
	for i := uintptr(0); i < n; i++ {
		crashByte(symName[i])
	}
	crashByte('+')
	crashHex(pc - s.starts[lo])
}

func heapSortUintptr(a []uintptr) {
	n := len(a)
	for i := n/2 - 1; i >= 0; i-- {
		siftDownUintptr(a, i, n)
	}
	for end := n - 1; end > 0; end-- {
		a[0], a[end] = a[end], a[0]
		siftDownUintptr(a, 0, end)
	}
}

func siftDownUintptr(a []uintptr, root, n int) {
	for {
		child := 2*root + 1
		if child >= n {
			return
		}
		if child+1 < n && a[child+1] > a[child] {
			child++
		}
		if a[root] >= a[child] {
			return
		}
		a[root], a[child] = a[child], a[root]
		root = child
	}
}

// The trace goes straight to stderr, and to the debug.SetCrashOutput file.
func crashFlush() {
	if crashLen > 0 {
		libc_write(2, unsafe.Pointer(&crashBuf[0]), uint(crashLen))
		if crashFD >= 0 && crashFD != 2 {
			libc_write(crashFD, unsafe.Pointer(&crashBuf[0]), uint(crashLen))
		}
		crashLen = 0
	}
}

func crashByte(c byte) {
	if crashLen == len(crashBuf) {
		crashFlush()
	}
	crashBuf[crashLen] = c
	crashLen++
}

func crashStr(s string) {
	for i := 0; i < len(s); i++ {
		crashByte(s[i])
	}
}

func crashHex(v uintptr) {
	crashStr("0x")
	started := false
	for shift := int(unsafe.Sizeof(v)*8) - 4; shift >= 0; shift -= 4 {
		d := byte(v>>uint(shift)) & 0xf
		if d == 0 && !started && shift != 0 {
			continue
		}
		started = true
		crashByte("0123456789abcdef"[d])
	}
}
