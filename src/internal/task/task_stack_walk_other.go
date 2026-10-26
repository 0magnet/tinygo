//go:build scheduler.tasks && linux && !baremetal && !amd64 && !arm64

package task

// walkStack reports no frames where TinyGo keeps no frame pointers.
func walkStack(sp, top uintptr, pcs []uintptr) int { return 0 }
