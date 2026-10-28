//go:build gc.boehm

package runtime

import (
	"internal/task"
	"math/bits"
	"unsafe"
)

// metricsStart is when the heap was set up, for /cpu/classes/total.
var metricsStart int64

// readMetrics fills v, metricCount long, with the metrics of this build by
// index, and pauses with the pause histogram, and returns a mask of the
// indexes it filled. It is used by runtime/metrics.
func readMetrics(v []uint64, pauses []uint64) uint64 {
	if len(v) < metricCount {
		return 0
	}
	var s libgc_prof_stats
	gcLock.Lock()
	libgc_get_prof_stats(&s, unsafe.Sizeof(s))
	live, trigger := gcMarked()
	gcLock.Unlock()

	objects, free, released, total := boehmHeapClasses(&s)
	v[metricHeapLive] = uint64(live)
	v[metricHeapGoal] = boehmHeapGoal(live, trigger)
	v[metricGoMemLimit] = uint64(gcMemoryLimit)
	v[metricGCCycles] = uint64(s.gc_no)
	v[metricHeapObjects] = objects
	v[metricHeapFree] = free
	v[metricHeapReleased] = released
	v[metricMemoryTotal] = total
	v[metricGoroutines] = uint64(task.NumGoroutine())
	mask := uint64(1)<<metricHeapLive | 1<<metricHeapGoal | 1<<metricGoMemLimit |
		1<<metricGCCycles | 1<<metricHeapObjects | 1<<metricHeapFree |
		1<<metricHeapReleased | 1<<metricMemoryTotal | 1<<metricGoroutines

	cpuTotal := float64(nanotime()-metricsStart) * gcProcs / 1e9
	v[metricCPUTotal] = float64bits(cpuTotal)
	mask |= 1 << metricCPUTotal
	if pauseNs, ok := gcReadPauseHist(pauses); ok {
		// Every core stops for a pause, so a pause costs its length on each.
		cpu := float64(pauseNs) * gcProcs / 1e9
		v[metricCPUGCPause] = float64bits(cpu)
		v[metricCPUGCTotal] = float64bits(cpu)
		mask |= 1<<metricCPUGCPause | 1<<metricCPUGCTotal | 1<<metricGCPauses
	}
	return mask
}

// boehmHeapClasses splits the bdwgc heap as Go's /memory/classes do. The
// objects include garbage not yet swept, as Go's do, and the small object
// free lists, which Go counts as unused.
func boehmHeapClasses(s *libgc_prof_stats) (objects, free, released, total uint64) {
	objects = uint64(s.heapsize_full - s.free_bytes_full)
	free = uint64(s.free_bytes_full - s.unmapped_bytes)
	released = uint64(s.unmapped_bytes)
	total = uint64(s.obtained_from_os_bytes)
	if heap := objects + free + released; total < heap {
		total = heap
	}
	return
}

// boehmHeapGoal is the heap size at which bdwgc collects again: the live heap
// and what it allocates before the next collection, or the heap cap of the
// memory limit if that comes first.
func boehmHeapGoal(live, trigger uintptr) uint64 {
	goal := uint64(live) + uint64(trigger)
	if c := gcHeapCapBytes(); c != 0 && uint64(c) < goal {
		goal = uint64(c)
	}
	return goal
}

// Pause histogram buckets: below 1 µs, then four per power of two up to
// 2^41 ns (about 37 minutes), and an overflow bucket.
const (
	gcPauseMinShift = 10
	gcPauseBuckets  = 1 + (41-gcPauseMinShift)*4 + 1
)

func gcPauseBucket(ns uint64) int {
	if ns < 1<<gcPauseMinShift {
		return 0
	}
	e := bits.Len64(ns) - 1
	i := 1 + (e-gcPauseMinShift)*4 + int(ns>>(e-2)&3)
	if i >= gcPauseBuckets {
		i = gcPauseBuckets - 1
	}
	return i
}

// metricsPauseBuckets fills b, gcPauseBuckets+1 long, with the bucket
// boundaries in seconds, and returns how many it filled.
func metricsPauseBuckets(b []float64) int {
	if len(b) < gcPauseBuckets+1 {
		return 0
	}
	b[0] = 0
	for i := 1; i < gcPauseBuckets; i++ {
		e := uint((i-1)/4 + gcPauseMinShift)
		ns := uint64(1)<<e + uint64((i-1)%4)<<(e-2)
		b[i] = float64(ns) / 1e9
	}
	b[gcPauseBuckets] = float64frombits(0x7ff0000000000000) // +Inf
	return gcPauseBuckets + 1
}

func metricsPauseBucketCount() int { return gcPauseBuckets }

//export tinygo_runtime_bdwgc_marked
func libgc_marked(trigger *uintptr) uintptr
