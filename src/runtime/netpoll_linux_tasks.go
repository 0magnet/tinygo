//go:build linux && scheduler.tasks && !baremetal && !nintendoswitch

package runtime

// With the tasks scheduler every goroutine shares one thread, so a goroutine
// that waits for a file descriptor parks here instead of blocking in a
// syscall. The scheduler polls epoll when it is idle, and every
// epollPollEvery task switches while goroutines are runnable.

import (
	"internal/task"
	"unsafe"
)

const (
	epollIn    = 0x1
	epollOut   = 0x4
	epollErr   = 0x8
	epollHup   = 0x10
	epollRdHup = 0x2000
	epollOne   = 1 << 30 // EPOLLONESHOT

	epollCtlAdd = 1
	epollCtlMod = 3

	errEINTR  = 4
	errENOENT = 2
	errEEXIST = 17
	errEPERM  = 1

	epollPollEvery = 64
)

// epollEvent is struct epoll_event. It is packed on amd64 and 386, so
// epollPad is zero there.
type epollEvent struct {
	events uint32
	_      [epollPad]byte
	data   [8]byte
}

func (e *epollEvent) fd() int32 {
	return *(*int32)(unsafe.Pointer(&e.data[0]))
}

func (e *epollEvent) setFD(fd int32) {
	*(*int32)(unsafe.Pointer(&e.data[0])) = fd
}

type epollWaiter struct {
	t     *task.Task
	f     *epollFile
	timer *timer
	ready bool
	done  bool
}

type epollFile struct {
	readers, writers []*epollWaiter
	registered       bool
}

var (
	epollFD      int32 = -1
	epollFiles   map[int32]*epollFile
	epollWaiting int
	epollSwitch  int
	epollEvents  [128]epollEvent
)

//export epoll_create1
func libc_epoll_create1(flags int32) int32

//export epoll_ctl
func libc_epoll_ctl(epfd, op, fd int32, ev *epollEvent) int32

//export epoll_wait
func libc_epoll_wait(epfd int32, evs *epollEvent, max, timeout int32) int32

// epollWaitFD parks the current goroutine until fd is readable (writable when
// write) or timeout nanoseconds pass, waiting forever when timeout is
// negative. It reports whether fd became ready.
func epollWaitFD(fd int32, write bool, timeout int64) bool {
	if epollFD < 0 {
		epollFD = libc_epoll_create1(0x80000) // EPOLL_CLOEXEC
		if epollFD < 0 {
			runtimeFatal("epoll_create1 failed")
		}
		epollFiles = make(map[int32]*epollFile)
	}
	f := epollFiles[fd]
	if f == nil {
		f = &epollFile{}
		epollFiles[fd] = f
	}
	w := &epollWaiter{t: task.Current(), f: f}
	if write {
		f.writers = append(f.writers, w)
	} else {
		f.readers = append(f.readers, w)
	}
	epollWaiting++
	if !epollArm(fd, f) {
		// epoll refuses regular files, which are always ready.
		epollWake(f, w, true)
		return true
	}
	if timeout >= 0 {
		w.timer = &timer{when: nanotime() + timeout, arg: w}
		addTimer(&timerNode{timer: w.timer, callback: epollTimeout})
	}
	task.Pause()
	return w.ready
}

// epollArm (re)programs epoll for the waiters of fd, and reports false if
// epoll cannot poll fd.
func epollArm(fd int32, f *epollFile) bool {
	var ev epollEvent
	if len(f.readers) != 0 {
		ev.events |= epollIn | epollRdHup
	}
	if len(f.writers) != 0 {
		ev.events |= epollOut
	}
	if ev.events == 0 {
		return true
	}
	ev.events |= epollOne
	ev.setFD(fd)
	op := int32(epollCtlAdd)
	if f.registered {
		op = epollCtlMod
	}
	for {
		if libc_epoll_ctl(epollFD, op, fd, &ev) == 0 {
			f.registered = true
			return true
		}
		switch *libc_errno_location() {
		case errENOENT: // closed and reused since the last wait
			op = epollCtlAdd
		case errEEXIST:
			op = epollCtlMod
		case errEPERM:
			return false
		default:
			return false
		}
	}
}

// epollWake removes w from f and makes its goroutine runnable.
func epollWake(f *epollFile, w *epollWaiter, ready bool) {
	if w.done {
		return
	}
	w.done = true
	w.ready = ready
	f.readers = epollRemove(f.readers, w)
	f.writers = epollRemove(f.writers, w)
	if w.timer != nil && ready {
		removeTimer(w.timer)
	}
	epollWaiting--
	scheduleTask(w.t)
}

func epollRemove(ws []*epollWaiter, w *epollWaiter) []*epollWaiter {
	for i, x := range ws {
		if x == w {
			copy(ws[i:], ws[i+1:])
			ws[len(ws)-1] = nil
			return ws[:len(ws)-1]
		}
	}
	return ws
}

func epollTimeout(tn *timerNode, delta int64) {
	w := tn.timer.arg.(*epollWaiter)
	epollWake(w.f, w, false)
}

// epollPoll wakes the goroutines whose fds are ready, waiting up to timeout
// milliseconds (forever when negative). A signal ends the wait early.
func epollPoll(timeout int32) {
	n := libc_epoll_wait(epollFD, &epollEvents[0], int32(len(epollEvents)), timeout)
	if n < 0 {
		if *libc_errno_location() == errEINTR && signalFutex.Swap(0) != 0 {
			checkSignals()
		}
		return
	}
	for i := int32(0); i < n; i++ {
		e := &epollEvents[i]
		fd := e.fd()
		f := epollFiles[fd]
		if f == nil {
			continue
		}
		hup := e.events&(epollErr|epollHup|epollRdHup) != 0
		if hup || e.events&epollIn != 0 {
			for len(f.readers) != 0 {
				epollWake(f, f.readers[0], true)
			}
		}
		if hup || e.events&epollOut != 0 {
			for len(f.writers) != 0 {
				epollWake(f, f.writers[0], true)
			}
		}
		// EPOLLONESHOT disarmed fd, so re-arm it for the waiters left.
		epollArm(fd, f)
	}
}

// schedulerPollTick polls without waiting every epollPollEvery task switches,
// so ready fds are served while goroutines keep the scheduler busy.
func schedulerPollTick() {
	if epollWaiting == 0 {
		return
	}
	epollSwitch++
	if epollSwitch >= epollPollEvery {
		epollSwitch = 0
		epollPoll(0)
	}
}

// epollIdle waits up to d for fds when the scheduler has nothing to run, and
// reports whether any goroutine waits on an fd.
func epollIdle(d timeUnit, forever bool) bool {
	if epollWaiting == 0 {
		return false
	}
	if signalFutex.Swap(0) != 0 {
		checkSignals()
		return true
	}
	ms := int32(-1)
	if !forever {
		ns := ticksToNanoseconds(d)
		if ns > 1e15 {
			ns = 1e15
		}
		ms = int32((ns + 999999) / 1e6)
	}
	epollPoll(ms)
	return true
}

//go:linkname os_runtime_waitFD os.runtime_waitFD
func os_runtime_waitFD(fd int32, write bool, timeout int64) bool {
	return epollWaitFD(fd, write, timeout)
}

//go:linkname net_runtime_waitFD net.runtime_waitFD
func net_runtime_waitFD(fd int32, write bool, timeout int64) bool {
	return epollWaitFD(fd, write, timeout)
}
