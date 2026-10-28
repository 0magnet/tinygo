package main

import (
	"bytes"
	"math"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"
)

var keep [][]byte

func main() {
	for i := 0; i < 256; i++ {
		keep = append(keep, make([]byte, 64<<10))
	}
	debug.SetMemoryLimit(1 << 40)
	for i := 0; i < 3; i++ {
		runtime.GC()
	}
	time.Sleep(10 * time.Millisecond)

	names := []string{
		"/gc/heap/live:bytes",
		"/gc/heap/goal:bytes",
		"/gc/gomemlimit:bytes",
		"/gc/cycles/total:gc-cycles",
		"/gc/pauses:seconds",
		"/sched/pauses/total/gc:seconds",
		"/cpu/classes/gc/pause:cpu-seconds",
		"/cpu/classes/gc/total:cpu-seconds",
		"/cpu/classes/total:cpu-seconds",
		"/memory/classes/heap/objects:bytes",
		"/memory/classes/heap/free:bytes",
		"/memory/classes/heap/released:bytes",
		"/memory/classes/total:bytes",
		"/sched/goroutines:goroutines",
		"/no/such/metric:bytes",
	}
	s := make([]metrics.Sample, len(names))
	for i, n := range names {
		s[i].Name = n
	}
	metrics.Read(s)
	v := map[string]metrics.Value{}
	for _, x := range s {
		v[x.Name] = x.Value
		if x.Value.Kind() == metrics.KindBad && x.Name != "/no/such/metric:bytes" {
			println("bad", x.Name)
		}
	}
	u := func(n string) uint64 { return v[n].Uint64() }
	f := func(n string) float64 { return v[n].Float64() }

	live := u("/gc/heap/live:bytes")
	census := censusLive()
	println("live", live >= 256*64<<10, live <= census+census/8, census <= live+live/8)
	println("goal", u("/gc/heap/goal:bytes") > live)
	println("limit", u("/gc/gomemlimit:bytes") == 1<<40)
	println("cycles", u("/gc/cycles/total:gc-cycles") >= 3)

	h := v["/gc/pauses:seconds"].Float64Histogram()
	var n uint64
	for _, c := range h.Counts {
		n += c
	}
	println("pauses", n >= 3, len(h.Buckets) == len(h.Counts)+1, math.IsInf(h.Buckets[len(h.Buckets)-1], 1))
	println("sched pauses", len(v["/sched/pauses/total/gc:seconds"].Float64Histogram().Counts) == len(h.Counts))

	pause, gc, total := f("/cpu/classes/gc/pause:cpu-seconds"), f("/cpu/classes/gc/total:cpu-seconds"), f("/cpu/classes/total:cpu-seconds")
	println("cpu", pause > 0, gc >= pause, total > gc)

	obj, free, rel, all := u("/memory/classes/heap/objects:bytes"), u("/memory/classes/heap/free:bytes"),
		u("/memory/classes/heap/released:bytes"), u("/memory/classes/total:bytes")
	println("memory", obj >= live, all >= obj+free+rel)
	println("goroutines", u("/sched/goroutines:goroutines") >= 1)
	println("unknown", v["/no/such/metric:bytes"].Kind() == metrics.KindBad)

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	println("memstats", ms.HeapSys == ms.HeapInuse+ms.HeapIdle, ms.HeapIdle >= ms.HeapReleased, ms.NextGC > 0, ms.NumGC >= 3, ms.GCCPUFraction > 0)

	all2 := metrics.All()
	found := 0
	for _, d := range all2 {
		for _, n := range names {
			if d.Name == n {
				found++
			}
		}
	}
	println("all", found == len(names)-1)
	runtime.KeepAlive(keep)
}

// censusLive reads the bytes the heap profile census counted as live.
func censusLive() uint64 {
	var buf bytes.Buffer
	pprof.Lookup("heap").WriteTo(&buf, 1)
	out := buf.String()
	const census = "# live census of the last collection: "
	if i := strings.Index(out, census); i >= 0 {
		n, _ := strconv.ParseUint(strings.Fields(out[i+len(census):])[0], 10, 64)
		return n
	}
	return 0
}
