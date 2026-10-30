//go:build arm && linux && !baremetal && !wasip1 && !wasip2 && !wasm_unknown

package runtime

import "unsafe"

// github.com/pion/transport/v5/utils/xor declares these without bodies and
// expects Go assembly, which TinyGo never assembles. XOR byte by byte.

//go:linkname pionXorBytesARM32 github.com/pion/transport/v5/utils/xor.xorBytesARM32
func pionXorBytesARM32(dst, a, b *byte, n int) { xorBytes(dst, a, b, n) }

//go:linkname pionXorBytesNEON32 github.com/pion/transport/v5/utils/xor.xorBytesNEON32
func pionXorBytesNEON32(dst, a, b *byte, n int) { xorBytes(dst, a, b, n) }

func xorBytes(dst, a, b *byte, n int) {
	for i := 0; i < n; i++ {
		*(*byte)(unsafe.Add(unsafe.Pointer(dst), i)) = *(*byte)(unsafe.Add(unsafe.Pointer(a), i)) ^ *(*byte)(unsafe.Add(unsafe.Pointer(b), i))
	}
}
