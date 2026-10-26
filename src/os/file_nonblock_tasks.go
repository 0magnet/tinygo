//go:build scheduler.tasks && linux && !baremetal && !wasm_unknown && !nintendoswitch && !wasip1 && !wasip2 && !tinygo.wasm

package os

import "time"

// waitFD parks the goroutine in the runtime poller.
func waitFD(fd int32, write bool, timeout time.Duration) bool {
	return runtime_waitFD(fd, write, int64(timeout))
}

func runtime_waitFD(fd int32, write bool, timeout int64) bool
