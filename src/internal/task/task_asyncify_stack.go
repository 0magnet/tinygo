//go:build scheduler.asyncify && !gc.leaking

package task

import "unsafe"

// allocStack allocates the stack of a new goroutine.
func allocStack(size uintptr) unsafe.Pointer {
	return takeStack(size)
}

// releaseStack is called with the stack of a goroutine that has exited.
func releaseStack(base, size uintptr) {
	putStack(unsafe.Pointer(base), size)
}
