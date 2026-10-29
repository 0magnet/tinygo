//go:build !scheduler.threads && !scheduler.cores

package runtime

func newTimerNode() *timerNode { return new(timerNode) }

func releaseTimerNode(*timerNode) {}
