//go:build scheduler.cores && linux && !baremetal && (gc.conservative || gc.precise || gc.boehm) && !boehm.threads

package runtime

import (
	"internal/futex"
	"internal/task"
)

var (
	gcStopped futex.Futex // 1 between the stop and gcResumeWorld
	gcAcks    futex.Futex // cores that have stopped
	gcGen     futex.Futex // bumped by every resume
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
		if i != me {
			markStoppedCore(i)
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
	gcReleaseIdleStacks()
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
	gcRecordCore()
	gcAcks.Add(1)
	gcAcks.Wake()
	for gcGen.Load() == gen {
		gcGen.Wait(gen)
	}
}
