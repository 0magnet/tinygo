package main

// bdwgc collected only when an allocation found no free block, so the heap
// filled up to whatever size it had grown to, about 1.8 times the live heap
// on the deployment host. With pacing it collects once GOGC percent of the
// marked bytes were allocated, as Go does. See gcPace in
// src/runtime/gc_boehm_threads.go.

import (
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strconv"
	"time"
)

type node struct {
	next *node
	refs []*node
	data []byte
}

// The live set. refs point at anchors, which never change, so a replaced
// node is garbage.
var table, anchors []*node

type rng uint64

func (r *rng) next() int {
	*r ^= *r << 13
	*r ^= *r >> 7
	*r ^= *r << 17
	return int(*r >> 1)
}

var worst time.Duration

// newNode allocates a mix like a server's: small objects with pointers, small
// and medium buffers, and a few large ones.
func newNode(r *rng) *node {
	t := time.Now()
	n := &node{}
	switch p := r.next() % 100; {
	case p < 70:
		n.data = make([]byte, 16+r.next()%512)
	case p < 95:
		n.data = make([]byte, 4096+r.next()%(28<<10))
	default:
		n.data = make([]byte, 128<<10+r.next()%(256<<10))
	}
	n.refs = make([]*node, 2+r.next()%30)
	if d := time.Since(t); d > worst {
		worst = d
	}
	for i := 0; i < len(n.data); i += 4096 {
		n.data[i] = 1
	}
	for i := range n.refs {
		n.refs[i] = anchors[r.next()%len(anchors)]
	}
	return n
}

var samples = []metrics.Sample{
	{Name: "/gc/heap/live:bytes"},
	{Name: "/gc/heap/goal:bytes"},
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/gc/cycles/total:gc-cycles"},
	{Name: "/cpu/classes/gc/total:cpu-seconds"},
	{Name: "/cpu/classes/total:cpu-seconds"},
	{Name: "/gc/pauses:seconds"},
	{Name: "/memory/classes/heap/free:bytes"},
}

func rss() float64 {
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
	return float64(n) * 4096
}

// The most heap objects, heap goal and resident memory per live byte, the
// share of the time the world was stopped, and the 99th percentile pause.
type phase struct {
	peak, goal, rss  float64
	gcShare, p99Wait float64
	live             float64
}

// run churns until gcs more collections ran, after settling for three.
func run(r *rng, gcs uint64) phase {
	var ph phase
	metrics.Read(samples)
	start := samples[3].Value.Uint64()
	var cpu0, gc0 float64
	var hist0 []uint64
	for {
		for k := 0; k < 64; k++ {
			i := r.next() % len(table)
			table[i] = newNode(r)
			_ = newNode(r)
		}
		metrics.Read(samples)
		live := float64(samples[0].Value.Uint64())
		n := samples[3].Value.Uint64()
		if n < start+3 || live == 0 {
			continue
		}
		if cpu0 == 0 {
			gc0, cpu0 = samples[4].Value.Float64(), samples[5].Value.Float64()
			hist0 = append([]uint64(nil), samples[6].Value.Float64Histogram().Counts...)
			continue
		}
		ph.live = live
		if x := float64(samples[2].Value.Uint64()) / live; x > ph.peak {
			ph.peak = x
		}
		if x := float64(samples[1].Value.Uint64()) / live; x > ph.goal {
			ph.goal = x
		}
		if x := rss() / live; x > ph.rss {
			ph.rss = x
		}
		if n >= start+3+gcs {
			break
		}
	}
	ph.gcShare = (samples[4].Value.Float64() - gc0) / (samples[5].Value.Float64() - cpu0)
	h := samples[6].Value.Float64Histogram()
	var total uint64
	counts := make([]uint64, len(h.Counts))
	for i := range h.Counts {
		counts[i] = h.Counts[i] - hist0[i]
		total += counts[i]
	}
	var seen uint64
	for i, c := range counts {
		seen += c
		if seen*100 >= total*99 {
			ph.p99Wait = h.Buckets[i+1]
			break
		}
	}
	return ph
}

func main() {
	r := rng(1)
	// About 220 MB live.
	anchors = make([]*node, 4096)
	for i := range anchors {
		anchors[i] = &node{}
	}
	table = make([]*node, 12000)
	for i := range table {
		table[i] = newNode(&r)
	}

	verbose := os.Getenv("VERBOSE") != ""
	failed := false
	check := func(name string, pct int, ph phase, want float64) {
		if verbose {
			println(name, pct, "peak objects/live", int(ph.peak*100), "% goal/live", int(ph.goal*100), "% rss/live", int(ph.rss*100), "% gc share", int(ph.gcShare*1000), "permille p99 pause", int(ph.p99Wait*1e3), "ms")
		}
		// The heap goal is the live heap and GOGC percent of it, and the
		// heap objects, which include garbage not yet swept, stay near it.
		if ph.goal < want*0.95 || ph.goal > want*1.05 {
			println(name, pct, "heap goal", int(ph.goal*100), "% of live, want", int(want*100), "%")
			failed = true
		}
		if ph.peak > want*1.1 {
			println(name, pct, "heap objects reached", int(ph.peak*100), "% of live, goal", int(want*100), "%")
			failed = true
		}
	}
	var peaks []float64
	for _, pct := range []int{50, 100, 200} {
		debug.SetGCPercent(pct)
		ph := run(&r, 12)
		check("gogc", pct, ph, 1+float64(pct)/100)
		peaks = append(peaks, ph.peak)
	}
	if !(peaks[0] < peaks[1] && peaks[1] < peaks[2]) {
		println("peak heap does not scale with GOGC:", int(peaks[0]*100), int(peaks[1]*100), int(peaks[2]*100))
		failed = true
	}

	// A memory limit that leaves room for 1.5 times the live heap holds the
	// heap there at GOGC=200.
	metrics.Read(samples)
	live := float64(samples[0].Value.Uint64())
	heap := float64(samples[2].Value.Uint64() + samples[7].Value.Uint64())
	other := rss() - heap
	if other < 0 {
		other = 0
	}
	debug.SetMemoryLimit(int64(other + 1.5*live))
	check("limit gogc", 200, run(&r, 12), 1.5)

	if worst > 300*time.Millisecond {
		println("slowest allocation took", worst.Milliseconds(), "ms")
		failed = true
	}
	if verbose {
		println("worst alloc", worst.Milliseconds(), "ms on", runtime.NumCPU(), "cpus")
	}
	if failed {
		os.Exit(1)
	}
	println("ok")
}
