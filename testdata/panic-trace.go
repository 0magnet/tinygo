package main

// Each case crashes in a function whose name the crash trace must show, or
// recovers, which must print no trace.

import (
	"os"
	"time"
	"unsafe"
)

type node struct {
	next *node
	val  int
}

// The address is hidden from the compiler so the load faults instead of
// taking a nil check.
var faultAddr = uintptr(len(os.Args) * 0x8)

// The bodies differ so that no linker or outliner pass folds them together.
//
//go:noinline
func faultInGoroutine() int { return (*[4]int)(unsafe.Pointer(faultAddr))[1] * 3 }

//go:noinline
func faultInMain() int { return (*[4]int)(unsafe.Pointer(faultAddr))[2] + 7 }

//go:noinline
func faultInDefer() int { return (*[4]int)(unsafe.Pointer(faultAddr))[3] ^ 5 }

//go:noinline
func nilCheckInGoroutine(n *node) int { return n.next.val }

//go:noinline
func explicitPanic() { panic("explicit panic here") }

//go:noinline
func blockedReceiver(ch chan int) { <-ch }

func main() {
	switch os.Args[1] {
	case "goroutine":
		go func() {
			defer func() { println("deferred ran") }()
			println(faultInGoroutine())
		}()
		time.Sleep(time.Second)
	case "main":
		println(faultInMain())
	case "defer":
		defer func() { println(faultInDefer()) }()
		println("main returns")
	case "nilcheck":
		go func() {
			defer func() { println("deferred ran") }()
			println(nilCheckInGoroutine(nil))
		}()
		time.Sleep(time.Second)
	case "explicit":
		explicitPanic()
	case "all":
		ch := make(chan int)
		for i := 0; i < 3; i++ {
			go blockedReceiver(ch)
		}
		time.Sleep(100 * time.Millisecond)
		println(faultInMain())
	case "recover":
		for i := 0; i < 3; i++ {
			func() {
				defer func() {
					if r := recover(); r != nil {
						println("recovered:", r.(error).Error())
					}
				}()
				println(faultInMain())
			}()
		}
		func() {
			defer func() { println("recovered:", recover().(string)) }()
			explicitPanic()
		}()
		println("done")
	}
}
