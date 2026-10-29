//go:build !(linux && !baremetal && !nintendoswitch && !wasip1 && !wasm_unknown && !wasip2)

package runtime

// Crash traces need the ELF symbol table and frame pointers of Linux.

func capturePanicTrace(pc, sig, code, fault uintptr) {}

func releasePanicTrace() {}

func printPanicTrace() {}

func printFatalTrace() {}

func printSignalTrace(sig, code, fault, pc, fp uintptr) {}
