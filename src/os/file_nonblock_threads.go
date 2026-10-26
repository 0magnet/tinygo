//go:build scheduler.threads && linux && !baremetal && !wasm_unknown && !nintendoswitch && !wasip1 && !wasip2 && !tinygo.wasm

package os

import "time"

// waitFD blocks the goroutine's own thread in ppoll.
func waitFD(fd int32, write bool, timeout time.Duration) bool {
	return ppollFD(fd, write, timeout)
}
