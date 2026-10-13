// Package metrics implements a subset of the Go runtime/metrics package.
// Names the runtime does not provide read as KindBad, as in Go.

package metrics

import _ "unsafe"

type Description struct {
	Name        string
	Description string
	Kind        ValueKind
	Cumulative  bool
}

//go:linkname metricsHeapLive runtime.metricsHeapLive
func metricsHeapLive() (uint64, bool)

//go:linkname metricsStackStats runtime.metricsStackStats
func metricsStackStats() ([7]uint64, bool)

// stackNames are the asyncify stack use names, in metricsStackStats order.
var stackNames = [7]string{
	"/tinygo/stack/max-c:bytes",
	"/tinygo/stack/max-save:bytes",
	"/tinygo/stack/sum-peak:bytes",
	"/tinygo/stack/over-16k:goroutines",
	"/tinygo/stack/over-32k:goroutines",
	"/tinygo/stack/over-64k:goroutines",
	"/tinygo/stack/over-128k:goroutines",
}

const heapLive = "/gc/heap/live:bytes"

func All() []Description {
	if _, ok := metricsHeapLive(); !ok {
		return nil
	}
	return []Description{{
		Name:        heapLive,
		Description: "Heap memory occupied by live objects that were marked by the previous GC.",
		Kind:        KindUint64,
	}}
}

type Float64Histogram struct {
	Counts  []uint64
	Buckets []float64
}

type Sample struct {
	Name  string
	Value Value
}

func Read(m []Sample) {
	for i := range m {
		m[i].Value = Value{}
		if m[i].Name == heapLive {
			if n, ok := metricsHeapLive(); ok {
				m[i].Value = Value{kind: KindUint64, scalar: n}
			}
		}
		for j, name := range stackNames {
			if m[i].Name == name {
				if v, ok := metricsStackStats(); ok {
					m[i].Value = Value{kind: KindUint64, scalar: v[j]}
				}
			}
		}
	}
}

type Value struct {
	kind   ValueKind
	scalar uint64
}

func (v Value) Float64() float64 {
	return 0
}

func (v Value) Float64Histogram() *Float64Histogram {
	return nil
}

func (v Value) Kind() ValueKind {
	return v.kind
}

func (v Value) Uint64() uint64 {
	return v.scalar
}

type ValueKind int

const (
	KindBad ValueKind = iota
	KindUint64
	KindFloat64
	KindFloat64Histogram
)
