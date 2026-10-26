//go:build (darwin || (linux && !baremetal && !wasip1 && !wasm_unknown && !wasip2 && !nintendoswitch)) && !(linux && scheduler.tasks)

package runtime

// epollIdle reports false: only the tasks scheduler on Linux polls fds.
func epollIdle(d timeUnit, forever bool) bool { return false }
