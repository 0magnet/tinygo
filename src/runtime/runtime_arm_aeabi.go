//go:build arm && linux && !baremetal

package runtime

// LLVM lowers unaligned 4 and 8 byte loads and stores on ARM to these helpers
// from the ARM run-time ABI, section 5.3.3, which compiler-rt does not have.
// The bytes are accessed one at a time through volatile operations, so LLVM
// cannot merge them back into the unaligned access that called the helper.

import (
	"runtime/volatile"
	"unsafe"
)

//export __aeabi_uread4
func aeabi_uread4(addr unsafe.Pointer) uint32 {
	var v uint32
	for i := uintptr(0); i < 4; i++ {
		v |= uint32(volatile.LoadUint8((*uint8)(unsafe.Add(addr, i)))) << (8 * i)
	}
	return v
}

//export __aeabi_uwrite4
func aeabi_uwrite4(v uint32, addr unsafe.Pointer) uint32 {
	for i := uintptr(0); i < 4; i++ {
		volatile.StoreUint8((*uint8)(unsafe.Add(addr, i)), uint8(v>>(8*i)))
	}
	return v
}

//export __aeabi_uread8
func aeabi_uread8(addr unsafe.Pointer) uint64 {
	var v uint64
	for i := uintptr(0); i < 8; i++ {
		v |= uint64(volatile.LoadUint8((*uint8)(unsafe.Add(addr, i)))) << (8 * i)
	}
	return v
}

//export __aeabi_uwrite8
func aeabi_uwrite8(v uint64, addr unsafe.Pointer) uint64 {
	for i := uintptr(0); i < 8; i++ {
		volatile.StoreUint8((*uint8)(unsafe.Add(addr, i)), uint8(v>>(8*i)))
	}
	return v
}
