//go:build scheduler.cores && !(linux && !baremetal)

package runtime

import "sync/atomic"

// schedulerPollTickLocked does nothing: only Linux has a poller.
func schedulerPollTickLocked() {}

// coresSysWait spins for a while if *addr == cmp: there is no kernel to sleep
// in. The caller checks again.
func coresSysWait(addr *uint32, cmp uint32) {
	for i := 0; atomic.LoadUint32(addr) == cmp && i < 100; i++ {
		spinLoopWait()
	}
}

func coresSysWake(addr *uint32) {}
