//go:build scheduler.cores && linux && !baremetal && (gc.conservative || gc.precise || gc.boehm)

package runtime

// The GC stops the other cores with a signal, its own or the bdwgc suspend
// signal. Each stopped core records where its stacks are in use, with its
// registers in the signal frame above that point, and sleeps until the GC
// resumes the world. The GC core scans those ranges itself, since the GC
// library may ask for the roots more than once in one collection.

import "internal/task"

var (
	// The in-use stack ranges of each stopped core: the stack it was
	// interrupted on, and its system stack while it ran a goroutine.
	gcCoreLow, gcCoreTop [numCPU]uintptr
	gcSysLow, gcSysTop   [numCPU]uintptr
	gcCoreTask           [numCPU]*task.Task

	gcResumes uint32
)

// Idle stack pages are released every few collections, since each stack costs
// a system call while the world is stopped.
const gcReleaseStacksEvery = 8

// gcRecordCore saves the stack ranges in use on the current core. It runs in
// the signal handler that stops the core.
func gcRecordCore() {
	core := currentCPU()
	sp := uintptr(stacksave())
	low, top := sp, coreStackTop(core)
	if t := task.Current(); t != nil {
		if tl, tt := task.MappedStackOf(t); sp >= tl && sp < tt {
			top = tt
		}
	}
	gcCoreLow[core], gcCoreTop[core] = low, top
	gcCoreTask[core] = task.Current()
	gcSysLow[core] = 0
	if ss := task.SystemStack(); top != coreStackTop(core) && ss != 0 && ss < coreStackTop(core) {
		gcSysLow[core], gcSysTop[core] = ss, coreStackTop(core)
	}
}

// markStoppedCore marks the stack ranges recorded by a stopped core.
func markStoppedCore(core uint32) {
	markRoots(gcCoreLow[core], gcCoreTop[core])
	if gcSysLow[core] != 0 {
		markRoots(gcSysLow[core], gcSysTop[core])
	}
}

// gcReleaseIdleStacks gives back idle stack pages every few collections. The
// world must be stopped.
func gcReleaseIdleStacks() {
	if gcResumes++; gcResumes%gcReleaseStacksEvery == 0 {
		me := currentCPU()
		gcCoreTask[me] = task.Current()
		task.ReleaseIdleStackPages(gcOnCore)
	}
}

// gcOnCore reports whether t was the current goroutine of a core when the world
// stopped. Its saved stack pointer is stale then.
func gcOnCore(t *task.Task) bool {
	for i := range gcCoreTask {
		if gcCoreTask[i] == t {
			return true
		}
	}
	return false
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
