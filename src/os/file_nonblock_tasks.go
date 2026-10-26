//go:build (scheduler.tasks || scheduler.cores) && linux && !baremetal && !wasm_unknown && !nintendoswitch && !wasip1 && !wasip2 && !tinygo.wasm

package os

import (
	"syscall"
	"time"
)

// waitFD parks the goroutine in the runtime poller.
func waitFD(fd int32, write bool, timeout time.Duration) bool {
	return runtime_waitFD(fd, write, int64(timeout))
}

func runtime_waitFD(fd int32, write bool, timeout int64) bool

// makePollable makes a character device, FIFO or socket non-blocking, as Go's
// os does, so a read parks in the poller instead of blocking every goroutine.
func (f *file) makePollable() {
	fd, ok := f.handle.(unixFileHandle)
	if !ok {
		return
	}
	var st syscall.Stat_t
	if syscall.Fstat(int(fd), &st) != nil {
		return
	}
	switch st.Mode & syscall.S_IFMT {
	case syscall.S_IFCHR, syscall.S_IFIFO, syscall.S_IFSOCK:
	default:
		return
	}
	if syscall.SetNonblock(int(fd), true) == nil {
		f.nb.madeNonblock = true
	}
}

// setBlocking undoes makePollable before the fd leaves os, for example to a
// child process. An fd that was non-blocking already stays so.
func (f *file) setBlocking() {
	if fd, ok := f.handle.(unixFileHandle); ok && f.nb.madeNonblock {
		syscall.SetNonblock(int(fd), false)
		f.nb.madeNonblock = false
	}
}
