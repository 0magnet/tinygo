package runtime

// Implementation of functions needed by runtime/metrics.
// Mostly just dummy implementations: we don't currently use any of these
// metrics.

//go:linkname godebug_registerMetric internal/godebug.registerMetric
func godebug_registerMetric(name string, read func() uint64) {
	// Dummy function for compatibility with Go 1.21.
}

// Metric indexes for readMetrics, in the order of the names in
// runtime/metrics.
const (
	metricHeapLive = iota
	metricHeapGoal
	metricGoMemLimit
	metricGCCycles
	metricGCPauses
	metricCPUGCPause
	metricCPUGCTotal
	metricCPUTotal
	metricHeapObjects
	metricHeapFree
	metricHeapReleased
	metricMemoryTotal
	metricGoroutines
	metricCount
)
