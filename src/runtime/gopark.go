package runtime

import (
	"internal/task"
	"unsafe"
)

// gopark and goready are Go runtime internals that code outside the runtime
// reaches through //go:linkname, gvisor's pkg/sync and pkg/sleep among it. The
// goroutine handle they trade is the task pointer. unlockf runs before the
// park and may decline it by returning false, as in Go.
func gopark(unlockf func(uintptr, unsafe.Pointer) bool, lock unsafe.Pointer, reason uint8, traceReason uint8, traceskip int) {
	t := task.Current()
	if unlockf != nil && !unlockf(uintptr(unsafe.Pointer(t)), lock) {
		return
	}
	task.Pause()
}

func goready(gp uintptr, traceskip int) {
	scheduleTask((*task.Task)(unsafe.Pointer(gp)))
}

// nmspinning stands in for Go's runtime.sched.nmspinning, which gvisor's
// pkg/sync reads through assembly on amd64 to decide whether a wake needs a
// new thread. TinyGo has no such count; the word is never read by the
// scheduler, so the adjustments gvisor makes to it are harmless.
var nmspinning int32

//go:linkname gvisorAddrOfSpinning gvisor.dev/gvisor/pkg/sync.addrOfSpinning
func gvisorAddrOfSpinning() *int32 { return &nmspinning }
