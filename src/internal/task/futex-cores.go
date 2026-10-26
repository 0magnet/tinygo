//go:build scheduler.cores

package task

import "runtime/interrupt"

// A futex is a way for userspace to wait with the pointer as the key, and for
// another thread to wake one or all waiting threads keyed on the same pointer.
//
// A futex does not change the underlying value, it only reads it before to prevent
// lost wake-ups.
type Futex struct {
	Uint32

	waiters Stack

	// Schedulers sleeping on the value, which have no task to park.
	sysWaiters Uint32
}

// Atomically check for cmp to still be equal to the futex value and if so, go
// to sleep. Return true if we were definitely awoken by a call to Wake or
// WakeAll, and false if we can't be sure of that.
func (f *Futex) Wait(cmp uint32) (awoken bool) {
	mask := lockFutex()

	if f.Uint32.Load() != cmp {
		unlockFutex(mask)
		return false
	}

	t := Current()
	if t == nil {
		// The scheduler runs timer callbacks without a task to park, so it
		// spins until the value changes.
		f.sysWaiters.Add(1)
		unlockFutex(mask)
		for i := 0; f.Uint32.Load() == cmp; i++ {
			if i < 100 {
				spinWait()
			} else {
				sysWait(&f.Uint32, cmp)
			}
		}
		f.sysWaiters.Add(^uint32(0))
		return false
	}

	// Push the current goroutine onto the waiter stack.
	f.waiters.Push(t)

	unlockFutex(mask)

	// Pause until this task is awoken by Wake/WakeAll.
	Pause()

	// We were awoken by a call to Wake or WakeAll. There is no chance for
	// spurious wakeups.
	return true
}

// Wake a single waiter.
func (f *Futex) Wake() {
	if f.sysWaiters.Load() != 0 {
		sysWake(&f.Uint32)
	}
	mask := lockFutex()
	t := f.waiters.Pop()
	unlockFutex(mask)
	if t != nil {
		scheduleTask(t)
	}
}

// Wake all waiters.
func (f *Futex) WakeAll() {
	if f.sysWaiters.Load() != 0 {
		sysWake(&f.Uint32)
	}
	mask := lockFutex()
	waiters := f.waiters.Queue()
	unlockFutex(mask)
	for t := waiters.Pop(); t != nil; t = waiters.Pop() {
		scheduleTask(t)
	}
}

//go:linkname lockFutex runtime.lockFutex
func lockFutex() interrupt.State

//go:linkname unlockFutex runtime.unlockFutex
func unlockFutex(interrupt.State)

//go:linkname spinWait runtime.spinLoopWait
func spinWait()

//go:linkname sysWait runtime.coresSysWait
func sysWait(addr *Uint32, cmp uint32)

//go:linkname sysWake runtime.coresSysWake
func sysWake(addr *Uint32)
