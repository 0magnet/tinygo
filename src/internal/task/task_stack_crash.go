//go:build (scheduler.tasks || scheduler.cores) && linux && !baremetal

package task

import "runtime/volatile"

// TryLockStacks takes the stack list lock if it frees up within spins tries.
// A crash may happen with the lock held, so it must not wait forever.
func TryLockStacks(spins int) bool {
	for i := 0; i < spins; i++ {
		if stackLock.CompareAndSwap(0, 1) {
			return true
		}
		spinWait()
	}
	return false
}

// NextStackTask returns the task after t in the list of tasks with a mapped
// stack, or the first one if t is nil. The caller holds the stack lock.
func NextStackTask(t *Task) *Task {
	if t == nil {
		return stackTasks
	}
	return t.state.stack.next
}

// ParkedStack fills pcs with the stack of a parked task t, innermost first,
// without allocating. It reports false when t runs on a core.
func ParkedStack(t *Task, pcs []uintptr) (reason WaitReason, n int, parked bool) {
	if t == Current() {
		return WaitRunning, 0, false
	}
	low, top := MappedStackOf(t)
	if !stackReleasedOnResume {
		if sp := t.state.sp; sp < low || sp >= top {
			return t.WaitReason, 0, true
		}
		return t.WaitReason, walkStack(t.state.sp, top, pcs), true
	}
	sp := parkedSP(t)
	if sp < low || sp >= top || volatile.LoadUint8(&t.RunState) != RunStatePaused {
		return WaitRunning, 0, false
	}
	reason = t.WaitReason
	n = walkStack(sp, top, pcs)
	if volatile.LoadUint8(&t.RunState) != RunStatePaused || parkedSP(t) != sp {
		return WaitRunning, 0, false
	}
	return reason, n, true
}
