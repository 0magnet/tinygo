//go:build gc.boehm

package runtime

import (
	"internal/gclayout"
	"internal/task"
	"unsafe"
)

// weakHandle is what a weak.Pointer refers to. It lives in pointer-free memory
// so ptr does not keep the object alive, and bdwgc clears ptr once the object
// is unreachable (a disappearing link).
type weakHandle struct {
	ptr uintptr
}

var (
	weakLock task.PMutex
	// weakHandles gives Make(p) == Make(p). Keys are ^p so that the map, which
	// the conservative GC scans, does not keep the objects alive.
	weakHandles    map[uintptr]*weakHandle
	weakSweepAfter = 64
)

//go:linkname registerWeakPointer weak.runtime_registerWeakPointer
func registerWeakPointer(ptr unsafe.Pointer) unsafe.Pointer {
	key := ^uintptr(ptr)
	weakLock.Lock()
	defer weakLock.Unlock()
	if h := weakHandles[key]; h != nil && weakLoad(h) != 0 {
		return unsafe.Pointer(h)
	}

	h := (*weakHandle)(alloc(unsafe.Sizeof(weakHandle{}), gclayout.NoPtrs.AsPtr()))
	h.ptr = uintptr(ptr)
	gcLock.Lock()
	var failed bool
	if base := libgc_base(uintptr(ptr)); base != 0 {
		failed = libgc_general_register_disappearing_link(unsafe.Pointer(&h.ptr), unsafe.Pointer(base)) != 0
	}
	gcResumeWorld()
	gcLock.Unlock()
	if failed {
		runtimeFatal("gc: out of memory")
	}

	if weakHandles == nil {
		weakHandles = make(map[uintptr]*weakHandle)
	}
	if len(weakHandles) >= weakSweepAfter {
		for k, old := range weakHandles {
			if weakLoad(old) == 0 {
				delete(weakHandles, k)
			}
		}
		weakSweepAfter = 2*len(weakHandles) + 64
	}
	weakHandles[key] = h
	return unsafe.Pointer(h)
}

//go:linkname makeStrongFromWeak weak.runtime_makeStrongFromWeak
func makeStrongFromWeak(u unsafe.Pointer) unsafe.Pointer {
	return unsafe.Pointer(weakLoad((*weakHandle)(u)))
}

// weakLoad reads h.ptr under the GC lock, so the collector cannot decide the
// object is dead between the read and the caller holding the pointer.
func weakLoad(h *weakHandle) uintptr {
	gcLock.Lock()
	p := h.ptr
	gcLock.Unlock()
	return p
}
