package main

import (
	"bytes"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
)

var keep [][]byte

//go:noinline
func keepB(n int) { keep = append(keep, make([]byte, n)) }

//go:noinline
func keepA(n int) { keepB(n) }

func main() {
	for i := 0; i < 64; i++ {
		keepA(64 << 10)
	}
	runtime.GC()
	var buf bytes.Buffer
	pprof.Lookup("heap").WriteTo(&buf, 1)
	out := buf.String()
	println(strings.Contains(out, " main.keepB<main.keepA<"))
	const census = "# live census of the last collection: "
	live := 0
	if i := strings.Index(out, census); i >= 0 {
		live, _ = strconv.Atoi(strings.Fields(out[i+len(census):])[0])
	}
	println(live >= 64<<16)
}
