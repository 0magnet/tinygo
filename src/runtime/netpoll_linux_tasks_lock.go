//go:build linux && scheduler.tasks && !baremetal && !nintendoswitch

package runtime

// With one thread the poller needs no lock and no wakeup fd.

type epollMutex struct{}

func (epollMutex) Lock()   {}
func (epollMutex) Unlock() {}

const wakeFD = -1

func epollDrainWake() {}

// signalWakeScheduler does nothing: the signal interrupts epoll_wait itself.
func signalWakeScheduler() {}

const epollPollers = 1

func epollPoller() int { return 0 }
