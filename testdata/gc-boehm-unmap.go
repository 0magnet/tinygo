package main

// After a burst the boehm heap kept every free block resident, since a block
// freed next to a free block makes it young again and bdwgc only gave back
// blocks free for three collections. See GC_unmap_old in
// src/runtime/gc_boehm_allchblk.c.

import (
	"os"
	"runtime"
	"strconv"
	"time"
)

var table [][]byte

type rng uint64

func (r *rng) next() int {
	*r ^= *r << 13
	*r ^= *r >> 7
	*r ^= *r << 17
	return int(*r >> 1)
}

// A mix of sizes: mostly small, some of a few blocks, a few large.
func size(r *rng) int {
	switch p := r.next() % 100; {
	case p < 70:
		return 16 + r.next()%512
	case p < 95:
		return 4096 + r.next()%(60<<10)
	default:
		return 256<<10 + r.next()%(768<<10)
	}
}

var worst time.Duration

func alloc(r *rng) []byte {
	n := size(r)
	t := time.Now()
	b := make([]byte, n)
	if d := time.Since(t); d > worst {
		worst = d
	}
	for i := 0; i < n; i += 4096 {
		b[i] = 1
	}
	return b
}

func rss() int {
	b, _ := os.ReadFile("/proc/self/statm")
	i := 0
	for i < len(b) && b[i] != ' ' {
		i++
	}
	j := i + 1
	for j < len(b) && b[j] != ' ' {
		j++
	}
	if j > len(b) {
		return 0
	}
	n, _ := strconv.Atoi(string(b[i+1 : j]))
	return n * 4096
}

// churn replaces random entries of the first n until gcs collections ran.
func churn(r *rng, n int, gcs uint32) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	end := ms.NumGC + gcs
	for ms.NumGC < end {
		for k := 0; k < 256; k++ {
			table[r.next()%n] = alloc(r)
			_ = alloc(r)
		}
		runtime.ReadMemStats(&ms)
	}
}

func main() {
	r := rng(1)
	base := rss()
	table = make([][]byte, 8000)

	// A burst to about 300 MB live, then a quarter of it.
	for i := range table {
		table[i] = alloc(&r)
	}
	churn(&r, len(table), 10)
	burst := rss() - base
	for i := len(table) / 4; i < len(table); i++ {
		table[i] = nil
	}
	churn(&r, len(table)/4, 40)

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	resident := rss() - base
	live := 0
	for _, b := range table {
		live += cap(b)
	}
	if os.Getenv("VERBOSE") != "" {
		println("burst", burst>>20, "MB resident", resident>>20, "MB live", live>>20, "MB heap", ms.HeapSys>>20, "MB released", ms.HeapReleased>>20, "MB worst alloc", worst.Milliseconds(), "ms")
	}
	if resident > 3*live {
		println("resident", resident>>20, "MB for", live>>20, "MB live")
		os.Exit(1)
	}
	if worst > 300*time.Millisecond {
		println("slowest allocation took", worst.Milliseconds(), "ms")
		os.Exit(1)
	}
	println("ok")
}
