//go:build gc.boehm && (scheduler.tasks || scheduler.cores) && linux && !baremetal && (amd64 || arm64)

package runtime

import (
	"internal/task"
)

// memProfCallers fills pcs with the call stack of an allocation whose
// allocator returns to pc, pc first. It walks frame pointers on a goroutine
// stack only, since only those have known bounds.
func memProfCallers(pc uintptr, pcs *[memProfDepth]uintptr) {
	pcs[0] = pc
	t := task.Current()
	if t == nil {
		return
	}
	low, top := task.MappedStackOf(t)
	if sp := uintptr(stacksave()); sp < low || sp >= top {
		return
	}
	var buf [memProfDepth + 8]uintptr
	n := int(tinygo_runtime_walk_frames(&buf[0], uintptr(len(buf)), top))
	for i := 0; i < n; i++ {
		if buf[i] == pc {
			copy(pcs[:], buf[i:n])
			return
		}
	}
}

//export tinygo_runtime_walk_frames
func tinygo_runtime_walk_frames(pcs *uintptr, n, top uintptr) uintptr
