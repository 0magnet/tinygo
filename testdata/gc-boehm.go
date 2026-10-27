package main

import (
	"runtime"
	"time"
	"unsafe"
	"weak"
)

type falseRootObject struct {
	data [64]byte
}

type inlineFalseRoot struct {
	value uintptr
	live  *byte
}

type externalFalseRoot struct {
	value   uintptr
	padding [64]uintptr
	live    *byte
}

var inlineRoot *inlineFalseRoot
var externalRoot *externalFalseRoot
var repeatedRoots []inlineFalseRoot
var falseRootObjectRef weak.Pointer[falseRootObject]
var frameSum uintptr

//go:noinline
func fillFrame(frame []uintptr) {
	for i := range frame {
		frame[i] = uintptr(i)
		frameSum += frame[i]
	}
}

// newFalseRootObject allocates depth frames down, leaving stale copies of
// the pointer below the stack pointer of the caller.
//
//go:noinline
func newFalseRootObject(depth int) *falseRootObject {
	var frame [4]uintptr
	fillFrame(frame[:])
	if depth == 0 {
		return new(falseRootObject)
	}
	object := newFalseRootObject(depth - 1)
	frameSum += frame[0]
	return object
}

//go:noinline
func makeFalseRoot(depth int, setRoot func(uintptr)) {
	object := newFalseRootObject(depth)
	address := uintptr(unsafe.Pointer(object))
	setRoot(address)
	falseRootObjectRef = weak.Make(object)
}

func expectCollected(depth int, setRoot func(uintptr)) {
	done := make(chan struct{})
	go func() {
		makeFalseRoot(depth, setRoot)
		close(done)
	}()
	<-done
	// A freed object may be reused on another thread, so check the object
	// itself rather than wait for its address to come back. A stale word on
	// an OS thread stack may hold it for a collection or two, so retry.
	for i := 0; i < 10; i++ {
		runtime.GC()
		if falseRootCollected() {
			return
		}
		clearStack()
		time.Sleep(time.Millisecond)
	}
	panic("non-pointer field retained allocation")
}

// falseRootCollected leaves the pointer Value returns in a frame that
// clearStack overwrites.
//
//go:noinline
func falseRootCollected() bool {
	return falseRootObjectRef.Value() == nil
}

//go:noinline
func clearStack() {
	var frame [256]uintptr
	fillFrame(frame[:])
}

func expectRepeatedPointersLive() {
	repeatedRoots = make([]inlineFalseRoot, 128)
	for i := range repeatedRoots {
		value := new(byte)
		*value = byte(i)
		repeatedRoots[i].live = value
	}
	runtime.GC()
	for i := range repeatedRoots {
		if *repeatedRoots[i].live != byte(i) {
			panic("repeated pointer field was not retained")
		}
	}
}

func main() {
	expectCollected(0, func(address uintptr) {
		inlineRoot = &inlineFalseRoot{
			value: address,
			live:  new(byte),
		}
	})
	expectCollected(0, func(address uintptr) {
		externalRoot = &externalFalseRoot{
			value: address,
			live:  new(byte),
		}
	})
	expectCollected(0, func(address uintptr) {
		repeatedRoots = make([]inlineFalseRoot, 128)
		repeatedRoots[100] = inlineFalseRoot{
			value: address,
			live:  new(byte),
		}
	})
	expectCollected(8, func(address uintptr) {
		inlineRoot = &inlineFalseRoot{
			value: address,
			live:  new(byte),
		}
	})
	expectRepeatedPointersLive()
	println("ok")
}
