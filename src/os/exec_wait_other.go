//go:build linux && !scheduler.tasks && !baremetal && !tinygo.wasm && !nintendoswitch

package os

// waitChild does nothing: the wait blocks only this goroutine's thread.
func waitChild(pid int) {}
