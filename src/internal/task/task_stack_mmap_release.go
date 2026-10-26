//go:build scheduler.tasks && linux && !baremetal

package task

// With one thread an exiting task can release its stack before it pauses.
const stackReleasedOnResume = false

func spinWait() {}
