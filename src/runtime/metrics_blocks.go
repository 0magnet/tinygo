//go:build gc.conservative || gc.precise

package runtime

// metricsHeapLive reports the heap bytes in use after the last collection,
// for /gc/heap/live:bytes in runtime/metrics.
func metricsHeapLive() (uint64, bool) {
	gcLock.Lock()
	n := gcLastLive
	gcLock.Unlock()
	return n, true
}
