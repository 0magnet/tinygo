package main

// A heap whose free lists hold many free blocks next to unmapped ones made
// GC_merge_unmapped walk the free lists again after every merge, so slice
// growth that found no free block large enough stalled every core for
// hundreds of milliseconds. See src/runtime/gc_boehm_allchblk.c.

import (
	"os"
	"runtime"
	"time"
)

var pairs, holes [][]byte

func blocks(n int) [][]byte {
	s := make([][]byte, n)
	for i := range s {
		s[i] = make([]byte, 3000) // one heap block each
	}
	return s
}

func main() {
	pairs = blocks(2 * 40000)
	holes = blocks(2 * 100000)

	// Free every other pair block, and let bdwgc unmap them.
	for i := 0; i < len(pairs); i += 2 {
		pairs[i] = nil
	}
	for i := 0; i < 5; i++ {
		runtime.GC()
	}
	// Free the rest, which bdwgc does not coalesce with their unmapped
	// neighbours, and every other hole block, which it cannot coalesce.
	for i := range pairs {
		pairs[i] = nil
	}
	for i := 0; i < len(holes); i += 2 {
		holes[i] = nil
	}
	runtime.GC()

	var worst time.Duration
	var sink [][]int
	for g := 0; g < 200; g++ {
		var s []int
		for i := 0; i < 64<<10; i++ {
			if len(s) < cap(s) {
				s = append(s, i)
				continue
			}
			t := time.Now()
			s = append(s, i)
			if d := time.Since(t); d > worst {
				worst = d
			}
		}
		sink = append(sink, s)
		if len(sink) > 8 {
			sink = sink[1:]
		}
	}
	// Merging costs a system call per pair, about 80 ms here. Walking the
	// lists again after every merge cost about 600 ms.
	if worst > 300*time.Millisecond {
		println("slowest append took", worst.Milliseconds(), "ms")
		os.Exit(1)
	}
	println("ok")
}
