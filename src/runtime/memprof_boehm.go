//go:build gc.boehm

package runtime

import "unsafe"

// A sampling heap profile. About every memProfRate bytes one allocation is
// recorded with its call stack. Boehm clears the sample's object pointer when
// the object is freed, so the samples still set are the live ones.

const (
	memProfRate  = 512 << 10
	memProfSlots = 1 << 14
	memProfSites = 1 << 13
	memProfDepth = 8
)

type memProfSample struct {
	obj    uintptr // a disappearing link, cleared by the GC on free
	site   uintptr
	weight uintptr
}

type memProfSite struct {
	pcs   [memProfDepth]uintptr
	alloc uintptr
}

var (
	memProfLeft    uintptr = memProfRate
	memProfCursor  uintptr
	memProfSamples *[memProfSlots]memProfSample
	memProfTable   *[memProfSites]memProfSite
)

// memProfRecord runs with gcLock held, before the world is resumed.
func memProfRecord(ptr unsafe.Pointer, size, pc uintptr) {
	if size < memProfLeft {
		memProfLeft -= size
		return
	}
	memProfLeft = memProfRate
	memProfTake(ptr, size, pc)
}

// memProfTake records one allocation in the heap profile, with gcLock held.
func memProfTake(ptr unsafe.Pointer, size, pc uintptr) {
	weight := uintptr(memProfRate)
	if size > weight {
		weight = size
	}
	if memProfSamples == nil {
		// Pointer free and uncollectable: the samples must not keep objects alive.
		s := libgc_malloc_atomic_uncollectable(unsafe.Sizeof(*memProfSamples))
		t := libgc_malloc_atomic_uncollectable(unsafe.Sizeof(*memProfTable))
		if s == nil || t == nil {
			return
		}
		memzero(s, unsafe.Sizeof(*memProfSamples))
		memzero(t, unsafe.Sizeof(*memProfTable))
		memProfSamples = (*[memProfSlots]memProfSample)(s)
		memProfTable = (*[memProfSites]memProfSite)(t)
		// Size the bdwgc disappearing link table for every slot now. A full
		// table makes bdwgc collect before it grows, see GC_grow_table, and as
		// most samples die that was a collection every thousand or so samples.
		for i := range memProfSamples {
			libgc_general_register_disappearing_link(unsafe.Pointer(&memProfSamples[i].obj), s)
		}
		for i := range memProfSamples {
			libgc_unregister_disappearing_link(unsafe.Pointer(&memProfSamples[i].obj))
		}
	}
	var pcs [memProfDepth]uintptr
	memProfCallers(pc, &pcs)
	h := uintptr(0)
	for _, p := range pcs {
		h = (h ^ p>>2) * 0x9e3779b1
	}
	site := uintptr(memProfSites)
	for i := uintptr(0); i < memProfSites; i++ {
		j := (h + i) % memProfSites
		if e := &memProfTable[j]; e.pcs == pcs || e.pcs[0] == 0 {
			e.pcs = pcs
			e.alloc += weight
			site = j
			break
		}
	}
	if site == memProfSites {
		return
	}
	for i := uintptr(0); i < memProfSlots; i++ {
		j := (memProfCursor + i) % memProfSlots
		s := &memProfSamples[j]
		if s.obj != 0 {
			continue
		}
		s.obj = uintptr(ptr)
		s.site = site
		s.weight = weight
		if libgc_general_register_disappearing_link(unsafe.Pointer(&s.obj), ptr) != 0 {
			s.obj = 0
		}
		memProfCursor = j + 1
		return
	}
}

// readMemProfile fills inuse and alloc (each memProfSites long) and pcs
// (memProfDepth per site, innermost first) with the sampled bytes per stack.
// It is used by runtime/pprof.
func readMemProfile(pcs, inuse, alloc []uintptr) int {
	if len(pcs) < memProfSites*memProfDepth || len(inuse) < memProfSites || len(alloc) < memProfSites {
		return memProfSites
	}
	gcLock.Lock()
	if memProfTable != nil {
		for i := range memProfTable {
			copy(pcs[i*memProfDepth:], memProfTable[i].pcs[:])
			alloc[i] = memProfTable[i].alloc
			inuse[i] = 0
		}
		for i := range memProfSamples {
			if s := &memProfSamples[i]; s.obj != 0 {
				inuse[s.site] += s.weight
			}
		}
	}
	gcLock.Unlock()
	return memProfSites
}

func setGCPercent(percent int32) int32 {
	gcLock.Lock()
	old := gcSetPercent(percent)
	gcLock.Unlock()
	return old
}

func freeOSMemory() {
	gcLock.Lock()
	libgc_gcollect_and_unmap()
	gcResumeWorld()
	gcLock.Unlock()
}

//export GC_general_register_disappearing_link
func libgc_general_register_disappearing_link(link, obj unsafe.Pointer) int32

//export GC_unregister_disappearing_link
func libgc_unregister_disappearing_link(link unsafe.Pointer) int32

//export GC_get_free_space_divisor
func libgc_get_free_space_divisor() uintptr

//export GC_set_free_space_divisor
func libgc_set_free_space_divisor(uintptr)

//export GC_gcollect_and_unmap
func libgc_gcollect_and_unmap()

//export GC_get_gc_no
func libgc_get_gc_no() uintptr

// poolGCCount is the number of collections so far, for sync.Pool.
func poolGCCount() uint32 { return uint32(libgc_get_gc_no()) }

// readMemProfileDepth is the number of pcs per site in readMemProfile.
func readMemProfileDepth() int { return memProfDepth }

// gcCensus mirrors struct tinygo_runtime_census in gc_boehm.c.
type gcCensus struct {
	kindBytes, kindObjects [8]uintptr
	sizeBytes, sizeObjects [32]uintptr
}

// readLiveCensus fills c, 80 words, with the bytes and objects marked by the
// last collection per bdwgc kind and per power of two size class.
func readLiveCensus(c []uintptr) bool {
	var g gcCensus
	if len(c) < int(unsafe.Sizeof(g)/unsafe.Sizeof(uintptr(0))) {
		return false
	}
	gcLock.Lock()
	gcAllocatorLock()
	tinygo_runtime_bdwgc_census(&g)
	gcAllocatorUnlock()
	gcLock.Unlock()
	copy(c, (*[80]uintptr)(unsafe.Pointer(&g))[:])
	return true
}

//export tinygo_runtime_bdwgc_census
func tinygo_runtime_bdwgc_census(c *gcCensus)
