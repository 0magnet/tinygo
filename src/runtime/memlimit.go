package runtime

// The soft memory limit, from GOMEMLIMIT or debug.SetMemoryLimit. Only the
// Boehm GC with thread support applies it, see gcGrowHeap.

const maxMemoryLimit = 1<<63 - 1

var gcMemoryLimit int64 = maxMemoryLimit

// setMemoryLimit sets the limit unless it is negative, and returns the
// previous one, as debug.SetMemoryLimit does.
func setMemoryLimit(limit int64) int64 {
	old := gcMemoryLimit
	if limit >= 0 {
		gcMemoryLimit = limit
	}
	return old
}
