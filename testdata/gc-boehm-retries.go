package main

// bdwgc aborted with "Too many retries in GC_alloc_large" in a server that
// allocated a 1.25 MB zstd encoder per response. Goroutine stacks are mapped
// next to the heap, and stale pointers to stacks that were unmapped
// black-list the pages where the heap grows next. A typed object needs a run
// of pages without one, and bdwgc ignores the black list only for objects
// bigger than GC_black_list_spacing, about 2 MB there. So every free block
// and every heap section it added failed, until GC_alloc_large gave up. See
// GC_allochblk in src/runtime/gc_boehm_allchblk.c.

import (
	"runtime"
	"runtime/debug"
	"unsafe"
)

// Like zstd's encoder: pointers and large tables.
type enc struct {
	next *enc
	hist []byte
	tab  [131072]uint64
	lt   [32768]uint32
}

type small struct {
	next *small
	b    [200]byte
}

// poison holds pointers to one page in every 512 KB below the heap, where
// its next sections are mapped.
var poison [4096]unsafe.Pointer

func main() {
	// Fill the heap with live data, so no clean free block is left.
	debug.SetGCPercent(10)
	var live *small
	for i := 0; i < 200000; i++ {
		live = &small{next: live}
	}
	low := uintptr(unsafe.Pointer(&enc{}))
	for i := 0; i < 64; i++ {
		if p := uintptr(unsafe.Pointer(&enc{})); p < low {
			low = p
		}
	}
	low &^= 1<<20 - 1
	for i := range poison {
		poison[i] = unsafe.Pointer(low - uintptr(i+1)<<19 + 0x800)
	}

	// Keep each encoder only through a pointer past its first page, which
	// must keep it alive.
	var kept []*uint32
	for i := 0; i < 200; i++ {
		e := &enc{hist: make([]byte, 1024)}
		for j := range e.lt {
			e.lt[j] = uint32(i)
		}
		kept = append(kept, &e.lt[len(e.lt)-1])
	}
	poison = [len(poison)]unsafe.Pointer{}
	runtime.GC()
	runtime.GC()
	// An encoder that was freed would be reused and overwritten here.
	for i := 0; i < 100; i++ {
		e := &enc{}
		for j := range e.lt {
			e.lt[j] = ^uint32(0)
		}
	}
	for i, p := range kept {
		e := (*enc)(unsafe.Add(unsafe.Pointer(p), -int(unsafe.Offsetof(enc{}.lt)+unsafe.Sizeof(enc{}.lt)-4)))
		for j := range e.lt {
			if e.lt[j] != uint32(i) {
				println("encoder", i, "was freed")
				return
			}
		}
	}
	println("ok", live != nil)
}
