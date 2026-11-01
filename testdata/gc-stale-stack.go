// Objects that a paused goroutine no longer uses must be collected, even when
// its stack still holds a word pointing to them. Such a word can be a GC stack
// slot whose pointer is dead, a dead local that asyncify saved, or memory of a
// frame that a deeper, earlier call wrote and the current frame has not.
package main

import (
	"runtime"
	"time"
)

const magic = 0x5a5a5a5a

type obj struct {
	magic uint32
	buf   [60]byte
}

var (
	block     = make(chan struct{})
	finalized int
	sink      int
	scratch   [64]*obj
)

func track(p *obj) {
	runtime.SetFinalizer(p, func(*obj) { finalized++ })
}

// deadSlot drops its object before it blocks.
//
//go:noinline
func deadSlot() {
	p := &obj{magic: magic}
	track(p)
	sink += int(p.buf[0])
	<-block
}

//go:noinline
func spread(a *[32]*obj) int {
	n := 0
	for _, p := range a {
		if p != nil {
			n++
		}
	}
	return n
}

// deep leaves copies of a pointer to its object in its frame.
//
//go:noinline
func deep() int {
	p := &obj{magic: magic}
	track(p)
	var arr [32]*obj
	for i := range arr {
		arr[i] = p
	}
	return spread(&arr)
}

// laterLocal blocks in frames laid over the ones deep used.
//
//go:noinline
func laterLocal() {
	<-block
	var a [32]*obj
	sink += spread(&a)
}

//go:noinline
func residue() {
	sink += deep()
	laterLocal()
}

//go:noinline
func churn() {
	for i := 0; i < 4; i++ {
		runtime.GC()
		for j := range scratch {
			scratch[j] = &obj{magic: 0xffffffff}
		}
	}
}

// lookup uses a value found in a map after the map itself is dead.
//
//go:noinline
func lookup() *obj {
	m := map[int]*obj{1: {magic: magic}}
	p := m[1]
	churn()
	return p
}

func main() {
	for i := 0; i < 4; i++ {
		go deadSlot()
		go residue()
	}
	time.Sleep(10 * time.Millisecond)
	for i := 0; i < 3; i++ {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	println("finalized", finalized, "of", 8)
	if p := lookup(); p.magic != magic {
		println("map value collected while in use, magic", p.magic)
	} else {
		println("map value ok")
	}
}
