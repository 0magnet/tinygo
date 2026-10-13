//go:build !gc.boehm

package runtime

func readMemProfile(pcs, inuse, alloc []uintptr) int { return 0 }

func setGCPercent(percent int32) int32 { return 100 }

func freeOSMemory() { GC() }
