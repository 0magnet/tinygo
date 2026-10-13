//go:build !scheduler.asyncify

package runtime

func metricsStackStats() (v [7]uint64, ok bool) { return v, false }
