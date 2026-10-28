//go:build gc.conservative || gc.precise

package runtime

import "internal/task"

func readMetrics(v []uint64, pauses []uint64) uint64 {
	if len(v) < metricCount {
		return 0
	}
	gcLock.Lock()
	v[metricHeapLive] = gcLastLive
	gcLock.Unlock()
	v[metricGoroutines] = uint64(task.NumGoroutine())
	return 1<<metricHeapLive | 1<<metricGoroutines
}

func metricsPauseBuckets(b []float64) int { return 0 }

func metricsPauseBucketCount() int { return 0 }
