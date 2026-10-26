//go:build scheduler.tasks && ((amd64 && !windows && !uefi) || arm64)

package task

var systemStack uintptr

// systemStackSlot returns where the scheduler's stack pointer is saved while a
// goroutine runs.
func systemStackSlot() *uintptr { return &systemStack }
