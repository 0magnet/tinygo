//go:build linux && (scheduler.tasks || scheduler.cores) && !baremetal && !nintendoswitch

package runtime

// Goroutines are tasks scheduled in user space, so a goroutine that waits for
// a file descriptor parks here instead of blocking a thread in a syscall. The
// scheduler polls epoll when it is idle, and every epollPollEvery task
// switches while goroutines are runnable. Under the cores scheduler several
// threads poll, so epollLock guards the waiter lists.

import (
	"internal/task"
	"sync/atomic"
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

// The waiter lists are linked through the waiters, and nothing under epollLock
// allocates: an allocation can park the task while it holds the lock.
type epollWaiter struct {
	t          *task.Task
	f          *epollFile
	timer      *timer
	prev, next *epollWaiter
	write      bool
	ready      bool
	done       bool
}

type epollFile struct {
	readers, writers *epollWaiter
	registered       bool
}

var (
	epollFD      int32        = -1
	epollFiles   []*epollFile // indexed by fd
	epollWaiting atomic.Int32
	epollSwitch  int
	epollLock    epollMutex

	// One event buffer per polling thread, since the scheduler must not
	// allocate.
	epollEvents [epollPollers][128]epollEvent
)

//export epoll_create1
func libc_epoll_create1(flags int32) int32

//export epoll_ctl
func libc_epoll_ctl(epfd, op, fd int32, ev *epollEvent) int32

//export epoll_wait
func libc_epoll_wait(epfd int32, evs *epollEvent, max, timeout int32) int32

// epollInit creates the epoll fd. It must be called with epollLock held or
// before other cores start.
func epollInit() {
	if epollFD >= 0 {
		return
	}
	epollFD = libc_epoll_create1(0x80000) // EPOLL_CLOEXEC
	if epollFD < 0 {
		runtimeFatal("epoll_create1 failed")
	}
}

// epollAdd watches fd for input for as long as it is open.
func epollAdd(fd int32) bool {
	ev := epollEvent{events: epollIn}
	ev.setFD(fd)
	return libc_epoll_ctl(epollFD, epollCtlAdd, fd, &ev) == 0
}

// epollWaitFD parks the current goroutine until fd is readable (writable when
// write) or timeout nanoseconds pass, waiting forever when timeout is
// negative. It reports whether fd became ready.
func epollWaitFD(fd int32, write bool, timeout int64) bool {
	w := &epollWaiter{t: task.Current(), write: write}
	var tn *timerNode
	if timeout >= 0 {
		w.timer = &timer{when: nanotime() + timeout, arg: w}
		tn = &timerNode{timer: w.timer, callback: epollTimeout}
	}
	f := epollLockFile(fd)
	w.f = f
	epollPush(w)
	if !epollArm(fd, f) {
		// epoll refuses regular files, which are always ready.
		epollUnlink(w)
		epollLock.Unlock()
		return true
	}
	epollWaiting.Add(1)
	epollLock.Unlock()

	// The timer goes in after the lock is released, since adding it may
	// allocate. A timer that fires after a wakeup finds the waiter done.
	if tn != nil {
		addTimer(tn)
	}

	// A core that sees fd ready before this pause marks the task to resume
	// at once.
	task.Pause()
	return w.ready
}

// epollLockFile takes epollLock and returns the entry for fd, growing the
// table with the lock released.
func epollLockFile(fd int32) *epollFile {
	for {
		epollLock.Lock()
		epollInit()
		n := len(epollFiles)
		if int(fd) < n && epollFiles[fd] != nil {
			return epollFiles[fd]
		}
		epollLock.Unlock()

		nf := &epollFile{}
		var grown []*epollFile
		if int(fd) >= n {
			size := 2 * n
			if size <= int(fd) {
				size = int(fd) + 64
			}
			grown = make([]*epollFile, size)
		}
		epollLock.Lock()
		if int(fd) >= len(epollFiles) && len(grown) > int(fd) {
			copy(grown, epollFiles)
			epollFiles = grown
		}
		if int(fd) < len(epollFiles) && epollFiles[fd] == nil {
			epollFiles[fd] = nf
		}
		epollLock.Unlock()
	}
}

// epollPush adds w to the readers or writers of its file.
func epollPush(w *epollWaiter) {
	head := &w.f.readers
	if w.write {
		head = &w.f.writers
	}
	w.next = *head
	if w.next != nil {
		w.next.prev = w
	}
	*head = w
}

// epollUnlink removes w from the list it is on.
func epollUnlink(w *epollWaiter) {
	if w.prev != nil {
		w.prev.next = w.next
	} else if w.write {
		w.f.writers = w.next
	} else {
		w.f.readers = w.next
	}
	if w.next != nil {
		w.next.prev = w.prev
	}
	w.prev, w.next = nil, nil
}

// epollArm (re)programs epoll for the waiters of fd, and reports false if
// epoll cannot poll fd.
func epollArm(fd int32, f *epollFile) bool {
	var ev epollEvent
	if f.readers != nil {
		ev.events |= epollIn | epollRdHup
	}
	if f.writers != nil {
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

// epollWake removes w from f and makes its goroutine runnable. It must be
// called with epollLock held.
func epollWake(f *epollFile, w *epollWaiter, ready bool) {
	if w.done {
		return
	}
	w.done = true
	w.ready = ready
	epollUnlink(w)
	if w.timer != nil && ready {
		removeTimer(w.timer)
	}
	epollWaiting.Add(-1)
	scheduleTask(w.t)
}

func epollTimeout(tn *timerNode, delta int64) {
	w := tn.timer.arg.(*epollWaiter)
	epollLock.Lock()
	epollWake(w.f, w, false)
	epollLock.Unlock()

	// Finish firing, so the threads and cores schedulers drop tn from their
	// list of firing timers.
	reAddTimer(tn)
}

// epollPoll wakes the goroutines whose fds are ready, waiting up to timeout
// milliseconds (forever when negative). A signal ends the wait early.
func epollPoll(timeout int32) {
	events := &epollEvents[epollPoller()]
	n := libc_epoll_wait(epollFD, &events[0], int32(len(events)), timeout)
	if n < 0 && *libc_errno_location() != errEINTR {
		return
	}
	if signalFutex.Swap(0) != 0 {
		checkSignals()
	}
	if n <= 0 {
		return
	}
	epollLock.Lock()
	for i := int32(0); i < n; i++ {
		e := &events[i]
		fd := e.fd()
		if fd == wakeFD {
			epollDrainWake()
			continue
		}
		var f *epollFile
		if fd >= 0 && int(fd) < len(epollFiles) {
			f = epollFiles[fd]
		}
		if f == nil {
			continue
		}
		hup := e.events&(epollErr|epollHup|epollRdHup) != 0
		if hup || e.events&epollIn != 0 {
			for f.readers != nil {
				epollWake(f, f.readers, true)
			}
		}
		if hup || e.events&epollOut != 0 {
			for f.writers != nil {
				epollWake(f, f.writers, true)
			}
		}
		// EPOLLONESHOT disarmed fd, so re-arm it for the waiters left.
		epollArm(fd, f)
	}
	epollLock.Unlock()
}

// schedulerPollTick polls without waiting every epollPollEvery task switches,
// so ready fds are served while goroutines keep the scheduler busy. It also
// delivers pending signals, which otherwise wait for an idle scheduler.
func schedulerPollTick() {
	epollSwitch++
	if epollSwitch < epollPollEvery {
		return
	}
	epollSwitch = 0
	if signalFutex.Swap(0) != 0 {
		checkSignals()
	}
	if epollWaiting.Load() != 0 {
		epollPoll(0)
	}
}

// epollIdle waits up to d for fds when the scheduler has nothing to run, and
// reports whether any goroutine waits on an fd.
func epollIdle(d timeUnit, forever bool) bool {
	if epollWaiting.Load() == 0 {
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
