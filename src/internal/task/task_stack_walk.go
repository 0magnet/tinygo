//go:build (scheduler.tasks || scheduler.cores) && linux && !baremetal && (amd64 || arm64)

package task

import "unsafe"

// walkStack fills pcs with the return addresses of a parked goroutine,
// innermost first, by following frame pointers from its saved registers. It
// stops at the first frame pointer outside [sp, top).
func walkStack(sp, top uintptr, pcs []uintptr) int {
	if len(pcs) == 0 {
		return 0
	}
	r := (*calleeSavedRegs)(unsafe.Pointer(sp))
	pcs[0] = r.pc
	n := 1
	for fp := savedFP(r); n < len(pcs) && fp >= sp && fp+16 <= top && fp%8 == 0; {
		pcs[n] = *(*uintptr)(unsafe.Pointer(fp + 8))
		n++
		next := *(*uintptr)(unsafe.Pointer(fp))
		if next <= fp {
			break
		}
		fp = next
	}
	return n
}
