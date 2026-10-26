//go:build scheduler.cores && linux && (amd64 || arm64)

package task

// systemStackSlot returns where this core's scheduler stack pointer is saved
// while a goroutine runs.
//
//go:linkname systemStackSlot runtime.systemStackPtr
func systemStackSlot() *uintptr
