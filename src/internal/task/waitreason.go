package task

// WaitReason says why a goroutine is not running, for the goroutine profile.
// The strings are those of Go's runtime.waitReason and goroutine statuses.
type WaitReason uint8

const (
	WaitNone WaitReason = iota
	WaitRunnable
	WaitRunning
	WaitChanReceive
	WaitChanSend
	WaitChanReceiveNil
	WaitChanSendNil
	WaitSelect
	WaitSelectNoCases
	WaitSleep
	WaitMutexLock
	WaitRWMutexLock
	WaitRWMutexRLock
	WaitCondWait
	WaitGroupWait
	WaitSemacquire
	WaitIO
	WaitSyscall
	WaitCoroutine
	WaitSynctest
	WaitFinalizer
)

var waitReasonStrings = [...]string{
	WaitNone:           "waiting",
	WaitRunnable:       "runnable",
	WaitRunning:        "running",
	WaitChanReceive:    "chan receive",
	WaitChanSend:       "chan send",
	WaitChanReceiveNil: "chan receive (nil chan)",
	WaitChanSendNil:    "chan send (nil chan)",
	WaitSelect:         "select",
	WaitSelectNoCases:  "select (no cases)",
	WaitSleep:          "sleep",
	WaitMutexLock:      "sync.Mutex.Lock",
	WaitRWMutexLock:    "sync.RWMutex.Lock",
	WaitRWMutexRLock:   "sync.RWMutex.RLock",
	WaitCondWait:       "sync.Cond.Wait",
	WaitGroupWait:      "sync.WaitGroup.Wait",
	WaitSemacquire:     "semacquire",
	WaitIO:             "IO wait",
	WaitSyscall:        "syscall",
	WaitCoroutine:      "coroutine",
	WaitSynctest:       "synctest.Wait",
	WaitFinalizer:      "finalizer wait",
}

func (r WaitReason) String() string {
	if int(r) < len(waitReasonStrings) {
		return waitReasonStrings[r]
	}
	return "waiting"
}

// SetWaitReason records why the current goroutine is about to pause. Pause
// clears it again when the goroutine resumes.
func SetWaitReason(r WaitReason) {
	if t := Current(); t != nil {
		t.WaitReason = r
	}
}
