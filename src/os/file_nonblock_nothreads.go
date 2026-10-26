//go:build !scheduler.threads && !scheduler.tasks && linux && !baremetal && !wasm_unknown && !nintendoswitch && !wasip1 && !wasip2 && !tinygo.wasm

package os

import "time"

// waitFD polls and sleeps, since goroutines share the thread and there is no
// runtime poller.
func waitFD(fd int32, write bool, timeout time.Duration) bool {
	if ppollFD(fd, write, 0) {
		return true
	}
	time.Sleep(time.Millisecond)
	return false
}
