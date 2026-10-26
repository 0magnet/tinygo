//go:build scheduler.cores && linux && !baremetal && (gc.conservative || gc.precise || gc.boehm)

package runtime

// The GC stops the other cores with a signal. Each stopped core records where
// its stacks are in use, with its registers in the signal frame above that
// point, and sleeps until the GC resumes the world. The GC core scans
// those ranges itself, since the GC library may ask for the roots more than
// once in one collection.

import (
	"internal/futex"
	"internal/task"
)

var (
	gcStopped futex.Futex // 1 between the stop and gcResumeWorld
	gcAcks    futex.Futex // cores that have stopped
	gcGen     futex.Futex // bumped by every resume

	// The in-use stack ranges of each stopped core: the stack it was
	// interrupted on, and its system stack while it ran a goroutine.
	gcCoreLow, gcCoreTop [numCPU]uintptr
	gcSysLow, gcSysTop   [numCPU]uintptr
)

func gcMarkReachable() {
	if secondaryCoresReady.Load() == 0 {
		scanCurrentStack()
		task.MarkParkedStacks(markRoots)
		markSystemStack(currentCPU())
		markGlobals()
		return
	}

	me := currentCPU()
	if gcStopped.Load() == 0 {
		// No core may start or end a goroutine while their stacks are
		// scanned. The lock is held until gcResumeWorld.
		task.LockStacks()
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
	}

	for i := uint32(0); i < numCPU; i++ {
		if i == me {
			continue
		}
		markRoots(gcCoreLow[i], gcCoreTop[i])
		if gcSysLow[i] != 0 {
			markRoots(gcSysLow[i], gcSysTop[i])
		}
	}
	scanCurrentStack()
	task.MarkParkedStacks(markRoots)
	markSystemStack(me)
	markGlobals()
}

// gcResumeWorld resumes the cores stopped by gcMarkReachable, if any.
func gcResumeWorld() {
	if gcStopped.Load() == 0 {
		return
	}
	gcStopped.Store(0)
	task.UnlockStacks()
	gcGen.Add(1)
	gcGen.WakeAll()
}

//export tinygo_cores_gc_pause
func coresGCPause(sig int32) {
	gen := gcGen.Load()
	if gcStopped.Load() == 0 {
		return
	}
	core := currentCPU()
	sp := uintptr(stacksave())
	low, top := sp, coreStackTop(core)
	if t := task.Current(); t != nil {
		if tl, tt := task.MappedStackOf(t); sp >= tl && sp < tt {
			top = tt
		}
	}
	gcCoreLow[core], gcCoreTop[core] = low, top
	gcSysLow[core] = 0
	if ss := task.SystemStack(); top != coreStackTop(core) && ss != 0 && ss < coreStackTop(core) {
		gcSysLow[core], gcSysTop[core] = ss, coreStackTop(core)
	}

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
