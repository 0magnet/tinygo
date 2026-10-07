//go:build gc.boehm

package runtime

import "unsafe"

// A sampling heap profile. About every memProfRate bytes one allocation is
// recorded with its call site. Boehm clears the sample's object pointer when
// the object is freed, so the samples still set are the live ones.

const (
	memProfRate  = 512 << 10
	memProfSlots = 1 << 14
	memProfSites = 1 << 12
)

type memProfSample struct {
	obj    uintptr // a disappearing link, cleared by the GC on free
	site   uintptr
	weight uintptr
}

type memProfSite struct {
	pc    uintptr
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
	}
	site := uintptr(memProfSites)
	for i := uintptr(0); i < memProfSites; i++ {
		j := (pc>>2 + i) % memProfSites
		if e := &memProfTable[j]; e.pc == pc || e.pc == 0 {
			e.pc = pc
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

// readMemProfile fills pcs, inuse and alloc (each memProfSites long) with the
// sampled bytes per call site. It is used by runtime/pprof.
func readMemProfile(pcs, inuse, alloc []uintptr) int {
	if len(pcs) < memProfSites || len(inuse) < memProfSites || len(alloc) < memProfSites {
		return memProfSites
	}
	gcLock.Lock()
	if memProfTable != nil {
		for i := range memProfTable {
			pcs[i] = memProfTable[i].pc
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
	old := int32(200 / libgc_get_free_space_divisor())
	if percent > 0 {
		// Boehm allows about 2*live/divisor bytes between collections, see
		// GC_free_space_divisor in lib/bdwgc/include/gc/gc.h.
		d := uintptr(200 / percent)
		if d < 1 {
			d = 1
		}
		libgc_set_free_space_divisor(d)
	}
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
