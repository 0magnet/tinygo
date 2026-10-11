package main

import "time"

//go:noinline
func panicHere(n int64, s string, f float64) {
	panic(s)
}

// An unusual signature, so the call to it gets a catcher of its own type.
var panicFunc = panicHere

var ch = make(chan int)

//go:noinline
func inner() {
	// The goroutine pauses here while the panic runs its defers.
	defer func() { <-ch }()
	panicFunc(1, "boom", 2)
}

func main() {
	go func() {
		time.Sleep(time.Millisecond)
		ch <- 1
	}()
	defer func() {}()
	inner()
}
