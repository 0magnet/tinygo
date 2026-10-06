//go:build amd64 || arm64

package runtime

// gvisor implements these in Go assembly on amd64 and arm64, which TinyGo
// does not assemble, so the bodies are supplied here by link name.

import (
	"internal/task"
	"math/bits"
	"sync/atomic"
	"unsafe"
)

//go:linkname gvisorAndUint32 gvisor.dev/gvisor/pkg/atomicbitops.andUint32
func gvisorAndUint32(addr *uint32, val uint32) {
	update32(addr, func(v uint32) uint32 { return v & val })
}

//go:linkname gvisorOrUint32 gvisor.dev/gvisor/pkg/atomicbitops.orUint32
func gvisorOrUint32(addr *uint32, val uint32) {
	update32(addr, func(v uint32) uint32 { return v | val })
}

//go:linkname gvisorXorUint32 gvisor.dev/gvisor/pkg/atomicbitops.xorUint32
func gvisorXorUint32(addr *uint32, val uint32) {
	update32(addr, func(v uint32) uint32 { return v ^ val })
}

//go:linkname gvisorCompareAndSwapUint32 gvisor.dev/gvisor/pkg/atomicbitops.compareAndSwapUint32
func gvisorCompareAndSwapUint32(addr *uint32, old, new uint32) uint32 {
	for {
		prev := atomic.LoadUint32(addr)
		if prev != old {
			return prev
		}
		if atomic.CompareAndSwapUint32(addr, old, new) {
			return old
		}
	}
}

//go:linkname gvisorAndUint64 gvisor.dev/gvisor/pkg/atomicbitops.andUint64
func gvisorAndUint64(addr *uint64, val uint64) {
	update64(addr, func(v uint64) uint64 { return v & val })
}

//go:linkname gvisorOrUint64 gvisor.dev/gvisor/pkg/atomicbitops.orUint64
func gvisorOrUint64(addr *uint64, val uint64) {
	update64(addr, func(v uint64) uint64 { return v | val })
}

//go:linkname gvisorXorUint64 gvisor.dev/gvisor/pkg/atomicbitops.xorUint64
func gvisorXorUint64(addr *uint64, val uint64) {
	update64(addr, func(v uint64) uint64 { return v ^ val })
}

//go:linkname gvisorCompareAndSwapUint64 gvisor.dev/gvisor/pkg/atomicbitops.compareAndSwapUint64
func gvisorCompareAndSwapUint64(addr *uint64, old, new uint64) uint64 {
	for {
		prev := atomic.LoadUint64(addr)
		if prev != old {
			return prev
		}
		if atomic.CompareAndSwapUint64(addr, old, new) {
			return old
		}
	}
}

//go:linkname gvisorTrailingZeros64 gvisor.dev/gvisor/pkg/bits.TrailingZeros64
func gvisorTrailingZeros64(x uint64) int { return bits.TrailingZeros64(x) }

//go:linkname gvisorMostSignificantOne64 gvisor.dev/gvisor/pkg/bits.MostSignificantOne64
func gvisorMostSignificantOne64(x uint64) int {
	if x == 0 {
		return 64
	}
	return 63 - bits.LeadingZeros64(x)
}

var gvisorFence uint32

//go:linkname gvisorMemoryFenceReads gvisor.dev/gvisor/pkg/sync.MemoryFenceReads
func gvisorMemoryFenceReads() { atomic.AddUint32(&gvisorFence, 0) }

// The task pointer is unique for as long as the goroutine lives, which is
// what gvisor needs from a goroutine id.
//
//go:linkname gvisorGoid gvisor.dev/gvisor/pkg/goid.goid
func gvisorGoid() int64 { return int64(uintptr(unsafe.Pointer(task.Current()))) }

// wakep asks Go for another thread to run work. Threads are not pooled here.
func wakep() {}

func goyield() { Gosched() }

func update32(addr *uint32, f func(uint32) uint32) {
	for {
		old := atomic.LoadUint32(addr)
		if atomic.CompareAndSwapUint32(addr, old, f(old)) {
			return
		}
	}
}

func update64(addr *uint64, f func(uint64) uint64) {
	for {
		old := atomic.LoadUint64(addr)
		if atomic.CompareAndSwapUint64(addr, old, f(old)) {
			return
		}
	}
}
