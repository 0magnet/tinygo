//go:build !gc.boehm

package runtime

import "unsafe"

// Without GC support for weak references the handle is the pointer itself, so
// a weak pointer keeps its object alive and never becomes nil.

//go:linkname registerWeakPointer weak.runtime_registerWeakPointer
func registerWeakPointer(ptr unsafe.Pointer) unsafe.Pointer {
	return ptr
}

//go:linkname makeStrongFromWeak weak.runtime_makeStrongFromWeak
func makeStrongFromWeak(u unsafe.Pointer) unsafe.Pointer {
	return u
}
