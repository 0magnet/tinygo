//go:build (amd64 || 386) && linux && !baremetal

package runtime

// golang.org/x/sys/cpu declares cpuid and xgetbv without bodies and expects Go
// assembly for them, which TinyGo never assembles. The bodies live in
// cpuid_x86.c and are linked in under the names that package expects.

//export tinygo_cpuid
func tinygo_cpuid(eax, ecx uint32, out *uint32)

//export tinygo_xgetbv
func tinygo_xgetbv(out *uint32)

//go:linkname sysCPUcpuid golang.org/x/sys/cpu.cpuid
func sysCPUcpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32) {
	var r [4]uint32
	tinygo_cpuid(eaxArg, ecxArg, &r[0])
	return r[0], r[1], r[2], r[3]
}

//go:linkname sysCPUxgetbv golang.org/x/sys/cpu.xgetbv
func sysCPUxgetbv() (eax, edx uint32) {
	var r [2]uint32
	tinygo_xgetbv(&r[0])
	return r[0], r[1]
}
