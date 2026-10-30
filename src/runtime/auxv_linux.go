//go:build linux && !baremetal && !wasip1 && !wasip2 && !wasm_unknown && !nintendoswitch

package runtime

const (
	_AT_NULL   = 0
	_AT_HWCAP  = 16
	_AT_HWCAP2 = 26
)

//export getauxval
func libc_getauxval(typ uintptr) uintptr

// getAuxv is linknamed from golang.org/x/sys/cpu, which reads the CPU
// features from the hardware capability entries.
func getAuxv() []uintptr {
	return []uintptr{
		_AT_HWCAP, libc_getauxval(_AT_HWCAP),
		_AT_HWCAP2, libc_getauxval(_AT_HWCAP2),
		_AT_NULL, 0,
	}
}
