//go:build scheduler.cores && linux && !baremetal && (gc.conservative || gc.precise || gc.boehm)

package runtime

// The GC stops the other cores with a signal. Each stopped core scans its own
// stack and registers in the signal handler, then sleeps until the GC resumes
// the world. Stacks of parked goroutines are scanned by the GC core.

import (
	"internal/futex"
	"internal/task"
)

var (
	gcStopped  futex.Futex // 1 between the stop and gcResumeWorld
	gcAcks     futex.Futex // cores that have scanned and stopped
	gcGen      futex.Futex // bumped by every resume
	gcScanLock spinLock    // the GC library is not thread safe
)

func gcMarkReachable() {
	if secondaryCoresReady.Load() == 0 {
		scanCurrentStack()
		task.MarkParkedStacks(markRoots)
		markSystemStack(currentCPU())
		markGlobals()
		return
	}

	// No core may start or end a goroutine while their stacks are scanned.
	task.LockStacks()
	me := currentCPU()
	gcAcks.Store(0)
	gcStopped.Store(1)
	for i := uint32(0); i < numCPU; i++ {
		if i != me {
			tinygo_cores_signal(coreThreads[i])
		}
	}
	for {
		n := gcAcks.Load()
		if n == numCPU-1 {
			break
		}
		gcAcks.Wait(n)
	}

	scanCurrentStack()
	task.MarkParkedStacks(markRoots)
	markSystemStack(me)
	markGlobals()
	task.UnlockStacks()
}

// gcResumeWorld resumes the cores stopped by gcMarkReachable, if any.
func gcResumeWorld() {
	if gcStopped.Load() == 0 {
		return
	}
	gcStopped.Store(0)
	gcGen.Add(1)
	gcGen.WakeAll()
}

//export tinygo_cores_gc_pause
func coresGCPause(sig int32) {
	gen := gcGen.Load()
	if gcStopped.Load() == 0 {
		return
	}
	gcScanLock.Lock()
	scanCurrentStack()
	markSystemStack(currentCPU())
	gcScanLock.Unlock()

	gcAcks.Add(1)
	gcAcks.Wake()
	for gcGen.Load() == gen {
		gcGen.Wait(gen)
	}
}

// markSystemStack marks the scheduler stack of core while a goroutine runs on
// it. A core on its system stack scans it with its registers instead.
func markSystemStack(core uint32) {
	top := coreStackTop(core)
	if sp := task.SystemStack(); sp != 0 && sp < top {
		markRoots(sp, top)
	}
}

//go:export tinygo_scanCurrentStack
func scanCurrentStack()

// scanstack is called by scanCurrentStack after pushing all registers. The
// stack is found from sp, since a core switching tasks has its current task
// set while it is still on the system stack, or the other way round.
//
//go:export tinygo_scanstack
func scanstack(sp uintptr) {
	if t := task.Current(); t != nil {
		if low, top := task.MappedStackOf(t); sp >= low && sp < top {
			markCurrentGoroutineStack(sp)
			return
		}
	}
	markRoots(sp, coreStackTop(currentCPU()))
}
