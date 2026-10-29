//go:build scheduler.cores

package task

// Unlock hands the lock to a waiter that has waited this long. Go uses 1 ms, but
// a wakeup here goes through the run queue, and 1 ms handoffs cut throughput.
const mutexStarveAfter = 10e6 // ns

// The futex holds 0 when unlocked, 1 when locked, and 2 when waiters need a wake.
type Mutex struct {
	futex Futex
}

func (m *Mutex) Lock() {
	if m.futex.CompareAndSwap(0, 1) {
		return
	}
	t := Current()
	if t == nil {
		// A scheduler running a timer callback has no task to park.
		for m.futex.Swap(2) != 0 {
			m.futex.Wait(2)
		}
		return
	}
	start := nanotime()
	woken := false
	for {
		mask := lockFutex()
		switch m.futex.Load() {
		case 0:
			if m.futex.CompareAndSwap(0, 2) {
				unlockFutex(mask)
				return
			}
			unlockFutex(mask)
			continue
		case 1:
			if !m.futex.CompareAndSwap(1, 2) {
				unlockFutex(mask)
				continue
			}
		}
		t.mutexWait.start = start
		if woken {
			// It lost the lock after a wakeup, so it keeps its place.
			m.futex.waiters.pushFront(t)
		} else {
			m.futex.waiters.Push(t)
		}
		unlockFutex(mask)
		SetWaitReason(WaitMutexLock)
		Pause()
		if t.mutexWait.start == 0 {
			return
		}
		woken = true
	}
}

func (m *Mutex) Unlock() {
	if m.futex.CompareAndSwap(1, 0) {
		return
	}
	mask := lockFutex()
	if m.futex.Load() == 0 {
		unlockFutex(mask)
		runtimeFatal("sync: unlock of unlocked Mutex")
	}
	t := m.futex.waiters.Pop()
	if t != nil && nanotime()-t.mutexWait.start >= mutexStarveAfter {
		// Hand the lock over: it stays held, and the value stays 2 so the
		// new owner wakes the next waiter.
		t.mutexWait.start = 0
	} else {
		m.futex.Store(0)
	}
	unlockFutex(mask)
	if m.futex.sysWaiters.Load() != 0 {
		sysWake(&m.futex.Uint32)
	}
	if t != nil {
		scheduleTask(t)
	}
}

// TryLock tries to lock m and reports whether it succeeded.
func (m *Mutex) TryLock() bool {
	return m.futex.CompareAndSwap(0, 1)
}

// pushFront adds t at the head of q.
func (q *Queue) pushFront(t *Task) {
	mask := lockAtomics()
	t.Next = q.head
	q.head = t
	if q.tail == nil {
		q.tail = t
	}
	unlockAtomics(mask)
}

//go:linkname nanotime runtime.nanotime
func nanotime() int64

// mutexWait is when a task started waiting for a Mutex, or 0 once handed it.
type mutexWait struct {
	start int64
}
