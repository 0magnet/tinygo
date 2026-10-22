package main

import (
	"runtime"
	"weak"
)

type obj struct {
	data [64]byte
	next *obj
}

//go:noinline
func makeDropped(n int) []weak.Pointer[obj] {
	ws := make([]weak.Pointer[obj], n)
	for i := range ws {
		ws[i] = weak.Make(&obj{next: &obj{}})
	}
	return ws
}

//go:noinline
func scrubStack(depth int) int {
	var buf [256]uintptr
	for i := range buf {
		buf[i] = 0
	}
	if depth > 0 {
		return scrubStack(depth-1) + int(buf[depth%256])
	}
	return 0
}

var global obj

func main() {
	// A conservative GC may keep a few objects alive through stale stack words.
	const n = 1000
	ws := makeDropped(n)
	scrubStack(16)
	for i := 0; i < 5; i++ {
		runtime.GC()
	}
	dead := 0
	for _, w := range ws {
		if w.Value() == nil {
			dead++
		}
	}
	println("dropped objects collected:", dead >= n*9/10)

	kept := &obj{}
	wk := weak.Make(kept)
	runtime.GC()
	println("kept object alive:", wk.Value() == kept)
	println("Make(p) == Make(p):", weak.Make(kept) == wk)
	println("distinct pointers differ:", weak.Make(&kept.data[1]) != weak.Make(&kept.data[2]))
	runtime.KeepAlive(kept)

	wg := weak.Make(&global)
	runtime.GC()
	println("global alive:", wg.Value() == &global)
}
