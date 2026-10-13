//go:build scheduler.asyncify

package runtime

import "internal/task"

// metricsStackStats reports task.StackStats for runtime/metrics.
func metricsStackStats() (v [7]uint64, ok bool) {
	maxC, maxSave, sum, over := task.StackStats()
	return [7]uint64{maxC, maxSave, sum, over[0], over[1], over[2], over[3]}, true
}
