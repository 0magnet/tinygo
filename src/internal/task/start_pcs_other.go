//go:build !scheduler.threads

package task

// GoroutineStartPCs reports nothing on schedulers that do not track it.
func GoroutineStartPCs(buf []uintptr) int { return 0 }
