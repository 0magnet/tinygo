//go:build scheduler.cores && !(linux && !baremetal)

package runtime

// schedulerPollTickLocked does nothing: only Linux has a poller.
func schedulerPollTickLocked() {}

// spinLoopYield waits a little during a long spin.
func spinLoopYield() {
	spinLoopWait()
}
