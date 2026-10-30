//go:build linux && (arm || arm64) && !baremetal && !wasip1 && !wasip2 && !wasm_unknown && !nintendoswitch

package runtime

import _ "unsafe"

//go:linkname cpuHWCap internal/cpu.HWCap
var cpuHWCap uint

// initHWCap stores the CPU features in internal/cpu.HWCap before package
// initializers run, as Go's runtime does. klauspost/cpuid reads it there.
func initHWCap() {
	cpuHWCap = uint(libc_getauxval(_AT_HWCAP))
}
