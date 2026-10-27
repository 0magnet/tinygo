//go:build scheduler.asyncify

package task

import "unsafe"

// The stacks of goroutines that have exited, kept for the next ones.
//
// On wasm every call from JavaScript into Go — each animation frame, timer
// and event handler — runs in a goroutine of its own, and each goroutine
// had a stack of its own allocated from the heap: 64 KB at the wasm target's
// default size, freed again when it exited. A page drawing at 60 frames a
// second allocated some 4 MB a second that way. Freeing it promptly did not
// make it cheap: a stack needs 64 KB in one piece, and between allocations
// the space it left was taken by smaller objects, so the next stack found
// no hole big enough. The heap was collected for want of one, and grown
// when collecting did not make one — chaosrack's TinyGo build held 79 MB of
// linear memory for 10 MB of live data, and collected eight times a minute.
//
// A few stacks kept here go round instead. Only as many as run at once are
// ever needed; past that a stack is freed as before.
const stackPoolMax = 8

var (
	// stackPool holds the kept stacks. It is a root for the GC, which is
	// what keeps them allocated. Only a conservative collector scans what is
	// in them (stackLayout), and they are cleared for it (putStack).
	stackPool     [stackPoolMax]unsafe.Pointer
	stackPoolSize [stackPoolMax]uintptr
	stackPoolLen  int
)

// takeStack is a stack of size bytes: a kept one if there is one that size,
// or a new one.
func takeStack(size uintptr) unsafe.Pointer {
	for i := stackPoolLen - 1; i >= 0; i-- {
		if stackPoolSize[i] != size {
			continue
		}
		p := stackPool[i]
		stackPoolLen--
		stackPool[i], stackPoolSize[i] = stackPool[stackPoolLen], stackPoolSize[stackPoolLen]
		stackPool[stackPoolLen], stackPoolSize[stackPoolLen] = nil, 0
		return p
	}
	return runtime_alloc(size, stackLayout())
}

// putStack keeps the stack of an exited goroutine for the next one, or
// frees it when enough are kept.
//
// A kept stack is cleared at once, so it is handed out as a new allocation
// would be. It matters to a collector that scans the stack's memory
// conservatively, as Boehm does: the words the goroutine left there would
// otherwise hold what they point at alive for as long as the stack is kept.
func putStack(p unsafe.Pointer, size uintptr) {
	if stackPoolLen < stackPoolMax {
		memzero(p, size)
		stackPool[stackPoolLen], stackPoolSize[stackPoolLen] = p, size
		stackPoolLen++
		return
	}
	runtime_freeTaskStack(uintptr(p))
}
