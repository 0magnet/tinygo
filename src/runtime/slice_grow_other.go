//go:build !gc.boehm

package runtime

import "unsafe"

// sliceGrowCap is the capacity for a slice of oldCap that needs newCap elements.
func sliceGrowCap(oldCap, newCap, elemSize uintptr, layout unsafe.Pointer) uintptr {
	return nextSliceCap(newCap, oldCap)
}

func sliceGrowFill(buf unsafe.Pointer, newCap, elemSize uintptr, layout unsafe.Pointer) uintptr {
	return newCap
}
