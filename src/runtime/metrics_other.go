//go:build !(gc.conservative || gc.precise || gc.boehm)

package runtime

import "internal/task"

func readMetrics(v []uint64, pauses []uint64) uint64 {
	if len(v) < metricCount {
		return 0
	}
	v[metricGoroutines] = uint64(task.NumGoroutine())
	return 1 << metricGoroutines
}

func metricsPauseBuckets(b []float64) int { return 0 }

func metricsPauseBucketCount() int { return 0 }
