package sync

import (
	"internal/task"
	"sync/atomic"
)

// runtime_Semacquire and runtime_Semrelease are Go's runtime semaphores, which
// code outside the standard library reaches through //go:linkname; gvisor's
// pkg/sync builds its Mutex on them. addr holds the count, as in Go. Waiters
// sleep on a futex found by address; the futex value is a generation counter
// that each release bumps, so a release between a waiter's check of addr and
// its sleep is not lost.

type semaWaiters struct {
	gen     task.Futex
	waiters int
}

var (
	semaLock  Mutex
	semaTable = map[*uint32]*semaWaiters{}
)

func runtime_Semacquire(addr *uint32) {
	for {
		if v := atomic.LoadUint32(addr); v > 0 {
			if atomic.CompareAndSwapUint32(addr, v, v-1) {
				return
			}
			continue
		}
		semaLock.Lock()
		w := semaTable[addr]
		if w == nil {
			w = &semaWaiters{}
			semaTable[addr] = w
		}
		w.waiters++
		gen := w.gen.Load()
		semaLock.Unlock()

		if atomic.LoadUint32(addr) == 0 {
			w.gen.Wait(gen)
		}

		semaLock.Lock()
		w.waiters--
		if w.waiters == 0 {
			delete(semaTable, addr)
		}
		semaLock.Unlock()
	}
}

func runtime_Semrelease(addr *uint32, handoff bool, skipframes int) {
	atomic.AddUint32(addr, 1)
	semaLock.Lock()
	if w := semaTable[addr]; w != nil {
		w.gen.Add(1)
		w.gen.Wake()
	}
	semaLock.Unlock()
}

// runtime_canSpin and runtime_doSpin are the spinning hints beside them. There
// is nothing to spin on here.
func runtime_canSpin(i int) bool { return false }

func runtime_doSpin() {}
