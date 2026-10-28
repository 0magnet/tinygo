package main

import (
	"bytes"
	"runtime/pprof"
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
	var buf bytes.Buffer
	pprof.Lookup("heap").WriteTo(&buf, 1)
	println(strings.Contains(buf.String(), " main.keepB<main.keepA<"))
}
