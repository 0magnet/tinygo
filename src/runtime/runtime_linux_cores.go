//go:build linux && scheduler.cores && !baremetal && !nintendoswitch && !wasip1 && !wasip2 && !wasm_unknown

package runtime

// The cores scheduler on Linux runs a scheduler loop on each of numCPU
// threads. One idle core waits in epoll for fds, timers and wakeups, and the
// other idle cores wait on a futex.

import (
	"internal/futex"
	"math/bits"
	"unsafe"
)

const numCPU = 4

// The scheduler loop of a secondary core runs timer callbacks on this stack.
const coreStackSize = 1 << 20

const noCore = ^uint32(0)

type threadID uintptr

var (
	schedulerLock spinLock
	futexLock     spinLock
	atomicsLock   spinLock
	printLock     spinLock

	coreThreads [numCPU]threadID
	coreTops    [numCPU]uintptr

	// The core waiting in epoll, and until when. Guarded by schedulerLock.
	sleepingCore = noCore
	sleepUntil   timeUnit
	sleepKicked  bool

	// Cores waiting on coreWake, as a bitset. Guarded by schedulerLock.
	waitingCores uint32
	coreWake     [numCPU]futex.Futex

	// Written to wake the core waiting in epoll.
	wakeFD int32 = -1

	pollTicks [numCPU]uint32
)

// spinLock is a futex mutex: 0 unlocked, 1 locked, 2 locked with waiters. A
// waiter spins briefly, then sleeps in the kernel, since threads that only
// yield to each other burn whole cores on a busy lock.
type spinLock struct {
	futex.Futex
}

func (l *spinLock) Lock() {
	if l.CompareAndSwap(0, 1) {
		return
	}
	for i := 0; i < 100; i++ {
		spinLoopWait()
		if l.Load() == 0 && l.CompareAndSwap(0, 1) {
			return
		}
	}
	for l.Swap(2) != 0 {
		l.Futex.Wait(2)
	}
}

func (l *spinLock) Unlock() {
	old := l.Swap(0)
	if schedulerAsserts && old == 0 {
		runtimeFatal("unlock of unlocked spinlock")
	}
	if old == 2 {
		l.Futex.Wake()
	}
}

type epollMutex = spinLock

func spinLoopWait() {
	tinygo_cores_spin()
}

func currentCPU() uint32 {
	return tinygo_cores_current()
}

// coreStackTop returns the top of the system stack of core.
func coreStackTop(core uint32) uintptr {
	if core == 0 {
		return stackTop
	}
	return coreTops[core]
}

// startSecondaryCores starts a thread for every core but the first.
func startSecondaryCores() {
	coresInitPoll()
	coresInitGC()
	for i := uint32(1); i < numCPU; i++ {
		if tinygo_cores_start(i, coreStackSize, &coreTops[i], &coreThreads[i]) != 0 {
			runtimeFatal("cannot start a scheduler thread")
		}
	}
}

//export tinygo_cores_run
func coresRun(cpu uint32) {
	coresRegisterGC(cpu)
	waitForSecondaryCoresReady()
	schedulerLock.Lock()
	scheduler(false)
	schedulerLock.Unlock()

	// main.main returned on this core.
	exit(0)
}

func hasSleepingCore() bool {
	return sleepingCore != noCore
}

// schedulerUnlockAndWait waits until another core wakes this one, or waits in
// epoll when no other core does. The scheduler lock is held on entry and on
// return.
func schedulerUnlockAndWait() {
	if sleepingCore == noCore {
		coresSleep(0, true)
		return
	}
	core := currentCPU()
	waitingCores |= 1 << core
	coreWake[core].Store(0)
	schedulerLock.Unlock()
	for coreWake[core].Load() == 0 {
		coreWake[core].Wait(0)
	}
	schedulerLock.Lock()
}

// sleepTicksMulticore waits in epoll for up to d. The scheduler lock is held
// on entry and on return.
func sleepTicksMulticore(d timeUnit) {
	coresSleep(d, false)
}

func coresSleep(d timeUnit, forever bool) {
	coresInitPoll()
	sleepingCore = currentCPU()
	sleepKicked = false
	ms := int32(-1)
	if forever {
		sleepUntil = timeUnit(^uint64(0) >> 1)
	} else {
		sleepUntil = ticks() + d
		ns := ticksToNanoseconds(d)
		if ns > 1e15 {
			ns = 1e15
		}
		ms = int32((ns + 999999) / 1e6)
	}
	schedulerLock.Unlock()
	epollPoll(ms)
	schedulerLock.Lock()
	sleepingCore = noCore

	// Pass the epoll wait to a waiting core, so fds stay watched while this
	// core runs goroutines.
	if waitingCores != 0 {
		schedulerWakeCore(uint32(bits.TrailingZeros32(waitingCores)))
	}
}

// interruptSleepTicksMulticore ends the epoll wait early if it lasts past
// wakeup. It must be called with the scheduler lock held.
func interruptSleepTicksMulticore(wakeup timeUnit) {
	if sleepingCore != noCore && !sleepKicked && wakeup < sleepUntil {
		sleepKicked = true
		coresKick()
	}
}

func coresKick() {
	one := uint64(1)
	libc_write(wakeFD, unsafe.Pointer(&one), 8)
}

// signalWakeScheduler wakes the core waiting in epoll from a signal handler.
func signalWakeScheduler() {
	if wakeFD >= 0 {
		coresKick()
	}
}

// epollDrainWake empties the eventfd after a wakeup.
func epollDrainWake() {
	var n uint64
	libc_read(wakeFD, unsafe.Pointer(&n), 8)
}

// Wake another core, if one is waiting or sleeping. Must be called with the
// scheduler lock held.
func schedulerWake() {
	if waitingCores != 0 {
		schedulerWakeCore(uint32(bits.TrailingZeros32(waitingCores)))
	} else if sleepingCore != noCore {
		interruptSleepTicksMulticore(0)
	}
}

// Wake the given core if it is waiting or sleeping. Must be called with the
// scheduler lock held.
func schedulerWakeCore(core uint32) {
	if waitingCores&(1<<core) != 0 {
		waitingCores &^= 1 << core
		coreWake[core].Store(1)
		coreWake[core].Wake()
	} else if sleepingCore == core {
		interruptSleepTicksMulticore(0)
	}
}

// schedulerPollTickLocked polls epoll without waiting every epollPollEvery
// passes of this core's scheduler loop, while no core waits in epoll. It must
// be called with the scheduler lock held, which it drops while polling.
func schedulerPollTickLocked() {
	core := currentCPU()
	pollTicks[core]++
	if pollTicks[core] < epollPollEvery || sleepingCore != noCore {
		return
	}
	pollTicks[core] = 0
	if epollWaiting.Load() == 0 && signalFutex.Load() == 0 {
		return
	}
	schedulerLock.Unlock()
	epollPoll(0)
	schedulerLock.Lock()
}

//export tinygo_cores_current
func tinygo_cores_current() uint32

//export tinygo_cores_init
func tinygo_cores_init(thread *threadID)

//export tinygo_cores_start
func tinygo_cores_start(cpu uint32, stackSize uintptr, top *uintptr, thread *threadID) int32

//export tinygo_cores_signal
func tinygo_cores_signal(thread threadID)

//export tinygo_cores_spin
func tinygo_cores_spin()

//export eventfd
func libc_eventfd(initval uint32, flags int32) int32

//export read
func libc_read(fd int32, buf unsafe.Pointer, count uint) int

// coresInitPoll creates the epoll fd and the wakeup eventfd. Only the first
// core runs before they exist.
func coresInitPoll() {
	if wakeFD >= 0 {
		return
	}
	epollInit()
	wakeFD = libc_eventfd(0, 0x80800) // EFD_CLOEXEC | EFD_NONBLOCK
	if wakeFD < 0 || !epollAdd(wakeFD) {
		runtimeFatal("cannot create the scheduler eventfd")
	}
}

const epollPollers = numCPU

func epollPoller() int { return int(currentCPU()) }

// coresSysWait sleeps while *addr == cmp, for a scheduler that waits on a
// task futex and has no task to park.
func coresSysWait(addr *uint32, cmp uint32) {
	(*futex.Futex)(unsafe.Pointer(addr)).Wait(cmp)
}

// coresSysWake wakes the schedulers sleeping in coresSysWait on addr.
func coresSysWake(addr *uint32) {
	(*futex.Futex)(unsafe.Pointer(addr)).WakeAll()
}
