package main

import (
	"bytes"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	mu   sync.Mutex
	rw   sync.RWMutex
	wg   sync.WaitGroup
	spin atomic.Bool
)

//go:noinline
func recvForever(c chan int) { <-c }

//go:noinline
func sendForever(c chan int) { c <- 1 }

//go:noinline
func sleeper() { time.Sleep(time.Hour) }

//go:noinline
func locker() { mu.Lock() }

//go:noinline
func rlocker() { rw.RLock() }

//go:noinline
func selector(a, b chan int) {
	select {
	case <-a:
	case <-b:
	}
}

//go:noinline
func spinner() {
	for !spin.Load() {
	}
}

func main() {
	c := make(chan int)
	for i := 0; i < 3; i++ {
		go recvForever(c)
	}
	go sendForever(make(chan int))
	go sleeper()
	mu.Lock()
	go locker()
	rw.Lock()
	go rlocker()
	go selector(make(chan int), make(chan int))
	go func() { select {} }()
	wg.Add(1)
	go func() { wg.Wait() }()
	go spinner()
	time.Sleep(200 * time.Millisecond)

	var buf bytes.Buffer
	pprof.Lookup("goroutine").WriteTo(&buf, 1)
	grouped := buf.String()
	buf.Reset()
	pprof.Lookup("goroutine").WriteTo(&buf, 2)
	each := buf.String()
	spin.Store(true)

	println(strings.Contains(grouped, "3 @ "), strings.Contains(grouped, "# [chan receive]\n#\t"))
	println(strings.Contains(grouped, "\tmain.recvForever\n"))
	for _, r := range []string{"chan receive", "chan send", "sleep", "sync.Mutex.Lock",
		"sync.RWMutex.RLock", "select", "select (no cases)", "sync.WaitGroup.Wait", "running"} {
		println(r, strings.Contains(grouped, "# ["+r+"]\n"), strings.Contains(each, " ["+r+"]:\n"))
	}
	println(strings.Count(each, "\ngoroutine ")+1 == 12, strings.Contains(each, "main.locker(...)\n"))
}
