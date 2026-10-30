//go:build arm64 && !baremetal

package runtime

// golang.org/x/sys/cpu declares these ID register reads without bodies and
// expects Go assembly, which TinyGo never assembles. Report no features. On
// linux the package reads them only when AT_HWCAP lacks HWCAP_CPUID.

//go:linkname sysCPUgetisar0 golang.org/x/sys/cpu.getisar0
func sysCPUgetisar0() uint64 { return 0 }

//go:linkname sysCPUgetisar1 golang.org/x/sys/cpu.getisar1
func sysCPUgetisar1() uint64 { return 0 }

//go:linkname sysCPUgetmmfr1 golang.org/x/sys/cpu.getmmfr1
func sysCPUgetmmfr1() uint64 { return 0 }

//go:linkname sysCPUgetpfr0 golang.org/x/sys/cpu.getpfr0
func sysCPUgetpfr0() uint64 { return 0 }

//go:linkname sysCPUgetzfr0 golang.org/x/sys/cpu.getzfr0
func sysCPUgetzfr0() uint64 { return 0 }
