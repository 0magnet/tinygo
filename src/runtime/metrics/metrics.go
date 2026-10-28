// Package metrics implements a subset of the Go runtime/metrics package.
// Names the runtime does not provide read as KindBad, as in Go.

package metrics

import (
	"math"
	_ "unsafe"
)

type Description struct {
	Name        string
	Description string
	Kind        ValueKind
	Cumulative  bool
}

// The metrics in the order of the indexes of runtime.readMetrics. The names,
// kinds and descriptions are Go's, see https://pkg.go.dev/runtime/metrics.
var descriptions = [...]Description{
	{
		Name:        "/gc/heap/live:bytes",
		Description: "Heap memory occupied by live objects that were marked by the previous GC.",
		Kind:        KindUint64,
	},
	{
		Name:        "/gc/heap/goal:bytes",
		Description: "Heap size target for the end of the GC cycle.",
		Kind:        KindUint64,
	},
	{
		Name:        "/gc/gomemlimit:bytes",
		Description: "Go runtime memory limit configured by the user, otherwise math.MaxInt64. This value is set by the GOMEMLIMIT environment variable, and the runtime/debug.SetMemoryLimit function.",
		Kind:        KindUint64,
	},
	{
		Name:        "/gc/cycles/total:gc-cycles",
		Description: "Count of all completed GC cycles.",
		Kind:        KindUint64,
		Cumulative:  true,
	},
	{
		Name:        "/gc/pauses:seconds",
		Description: "Deprecated. Prefer the identical /sched/pauses/total/gc:seconds.",
		Kind:        KindFloat64Histogram,
		Cumulative:  true,
	},
	{
		Name:        "/cpu/classes/gc/pause:cpu-seconds",
		Description: "Estimated total CPU time spent with the application paused by the GC. Even if only one thread is running during the pause, this is computed as GOMAXPROCS times the pause latency because nothing else can be executing. This is the exact sum of samples in /sched/pauses/total/gc:seconds if each sample is multiplied by GOMAXPROCS at the time it is taken. This metric is an overestimate, and not directly comparable to system CPU time measurements. Compare only with other /cpu/classes metrics.",
		Kind:        KindFloat64,
		Cumulative:  true,
	},
	{
		Name:        "/cpu/classes/gc/total:cpu-seconds",
		Description: "Estimated total CPU time spent performing GC tasks. This metric is an overestimate, and not directly comparable to system CPU time measurements. Compare only with other /cpu/classes metrics. Sum of all metrics in /cpu/classes/gc.",
		Kind:        KindFloat64,
		Cumulative:  true,
	},
	{
		Name:        "/cpu/classes/total:cpu-seconds",
		Description: "Estimated total available CPU time for user Go code or the Go runtime, as defined by GOMAXPROCS. In other words, GOMAXPROCS integrated over the wall-clock duration this process has been executing for. This metric is an overestimate, and not directly comparable to system CPU time measurements. Compare only with other /cpu/classes metrics. Sum of all metrics in /cpu/classes.",
		Kind:        KindFloat64,
		Cumulative:  true,
	},
	{
		Name:        "/memory/classes/heap/objects:bytes",
		Description: "Memory occupied by live objects and dead objects that have not yet been marked free by the garbage collector.",
		Kind:        KindUint64,
	},
	{
		Name:        "/memory/classes/heap/free:bytes",
		Description: "Memory that is completely free and eligible to be returned to the underlying system, but has not been. This metric is the runtime's estimate of free address space that is backed by physical memory.",
		Kind:        KindUint64,
	},
	{
		Name:        "/memory/classes/heap/released:bytes",
		Description: "Memory that is completely free and has been returned to the underlying system.",
		Kind:        KindUint64,
	},
	{
		Name:        "/memory/classes/total:bytes",
		Description: "All memory mapped by the Go runtime into the current process as read-write. Note that this does not include memory mapped by code called via cgo or via the syscall package. Sum of all metrics in /memory/classes.",
		Kind:        KindUint64,
	},
	{
		Name:        "/sched/goroutines:goroutines",
		Description: "Count of live goroutines.",
		Kind:        KindUint64,
	},
}

const metricGCPauses = 4

// The pause histogram under its current name.
var schedPauses = Description{
	Name:        "/sched/pauses/total/gc:seconds",
	Description: "Distribution of individual GC-related stop-the-world pause latencies. This is the time from deciding to stop the world until the world is started again. Some of this time is spent getting all threads to stop (this is measured directly in /sched/pauses/stopping/gc:seconds), during which some threads may still be running. Bucket counts increase monotonically.",
	Kind:        KindFloat64Histogram,
	Cumulative:  true,
}

//go:linkname readMetrics runtime.readMetrics
func readMetrics(v []uint64, pauses []uint64) uint64

//go:linkname pauseBuckets runtime.metricsPauseBuckets
func pauseBuckets(b []float64) int

//go:linkname pauseBucketCount runtime.metricsPauseBucketCount
func pauseBucketCount() int

// All returns the metrics this build provides, sorted by name as in Go.
func All() []Description {
	var v [len(descriptions)]uint64
	mask := readMetrics(v[:], nil)
	var out []Description
	for i, d := range descriptions {
		if mask&(1<<i) != 0 {
			out = append(out, d)
			if i == metricGCPauses {
				out = append(out, schedPauses)
			}
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Name < out[j-1].Name; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

type Float64Histogram struct {
	Counts  []uint64
	Buckets []float64
}

type Sample struct {
	Name  string
	Value Value
}

// Read fills each sample with the value of its metric, from one snapshot of
// the runtime, or KindBad for a metric this build does not provide.
func Read(m []Sample) {
	var v [len(descriptions)]uint64
	var pauses []uint64
	for i := range m {
		if m[i].Name == descriptions[metricGCPauses].Name || m[i].Name == schedPauses.Name {
			pauses = make([]uint64, pauseBucketCount())
			break
		}
	}
	mask := readMetrics(v[:], pauses)
	for i := range m {
		old := m[i].Value.hist
		m[i].Value = Value{}
		idx := -1
		if m[i].Name == schedPauses.Name {
			idx = metricGCPauses
		}
		for j := range descriptions {
			if descriptions[j].Name == m[i].Name {
				idx = j
				break
			}
		}
		if idx < 0 || mask&(1<<idx) == 0 {
			continue
		}
		switch k := descriptions[idx].Kind; k {
		case KindFloat64Histogram:
			// Reuse the histogram of the previous Read, as Go does.
			h := old
			if h == nil {
				h = &Float64Histogram{}
			}
			h.Counts = append(h.Counts[:0], pauses...)
			if len(h.Buckets) != len(pauses)+1 {
				h.Buckets = make([]float64, len(pauses)+1)
				pauseBuckets(h.Buckets)
			}
			m[i].Value = Value{kind: k, hist: h}
		default:
			m[i].Value = Value{kind: k, scalar: v[idx]}
		}
	}
}

type Value struct {
	kind   ValueKind
	scalar uint64
	hist   *Float64Histogram
}

// Float64 returns the value of a KindFloat64 metric, and panics otherwise.
func (v Value) Float64() float64 {
	if v.kind != KindFloat64 {
		panic("called Float64 on non-float64 metric value")
	}
	return math.Float64frombits(v.scalar)
}

// Float64Histogram returns the value of a KindFloat64Histogram metric, and
// panics otherwise.
func (v Value) Float64Histogram() *Float64Histogram {
	if v.kind != KindFloat64Histogram {
		panic("called Float64Histogram on non-Float64Histogram metric value")
	}
	return v.hist
}

func (v Value) Kind() ValueKind {
	return v.kind
}

// Uint64 returns the value of a KindUint64 metric, and panics otherwise.
func (v Value) Uint64() uint64 {
	if v.kind != KindUint64 {
		panic("called Uint64 on non-uint64 metric value")
	}
	return v.scalar
}

type ValueKind int

const (
	KindBad ValueKind = iota
	KindUint64
	KindFloat64
	KindFloat64Histogram
)
