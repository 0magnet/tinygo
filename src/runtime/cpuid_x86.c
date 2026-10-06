//go:build none

// Included on linux/386 and linux/amd64 (see compileopts/target.go). The
// bodies behind golang.org/x/sys/cpu's cpuid and xgetbv, see sys_cpu_x86.go.

#include <stdint.h>
#include <cpuid.h>

void tinygo_cpuid(uint32_t eax, uint32_t ecx, uint32_t *out) {
	unsigned a = 0, b = 0, c = 0, d = 0;
	__cpuid_count(eax, ecx, a, b, c, d);
	out[0] = a;
	out[1] = b;
	out[2] = c;
	out[3] = d;
}

void tinygo_xgetbv(uint32_t *out) {
	uint32_t eax, edx;
	__asm__ volatile("xgetbv" : "=a"(eax), "=d"(edx) : "c"(0));
	out[0] = eax;
	out[1] = edx;
}
