//go:build linux && (scheduler.tasks || scheduler.cores) && !baremetal && !tinygo.wasm && !nintendoswitch

package os

import (
	"syscall"
	"time"
	"unsafe"
)

const sysPidfdOpen = 434 // the same number on every Linux architecture

// waitChild parks until pid has exited, so the wait that follows does not
// block the thread every goroutine shares.
func waitChild(pid int) {
	fd, _, e := syscall.Syscall(sysPidfdOpen, uintptr(pid), 0, 0)
	if e == 0 {
		for !runtime_waitFD(int32(fd), false, -1) {
		}
		syscall.Close(int(fd))
		return
	}
	// No pidfd (Linux before 5.3): poll for the exit.
	var info [128]byte
	const pidOff = 12 + 4*(unsafe.Sizeof(uintptr(0))/8) // si_pid follows the siginfo preamble
	for {
		info = [128]byte{} // si_pid stays zero while the child runs
		_, _, e := syscall.Syscall6(syscall.SYS_WAITID, 1 /* P_PID */, uintptr(pid),
			uintptr(unsafe.Pointer(&info[0])), syscall.WEXITED|syscall.WNOWAIT|syscall.WNOHANG, 0, 0)
		if e != 0 && e != syscall.EINTR {
			return
		}
		if e == 0 && *(*int32)(unsafe.Pointer(&info[pidOff])) != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
