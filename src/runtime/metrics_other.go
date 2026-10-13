//go:build !(gc.conservative || gc.precise)

package runtime

func metricsHeapLive() (uint64, bool) { return 0, false }
