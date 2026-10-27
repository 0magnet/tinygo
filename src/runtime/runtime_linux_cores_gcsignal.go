//go:build linux && scheduler.cores && !baremetal && !nintendoswitch && !wasip1 && !wasip2 && !wasm_unknown && !boehm.threads

package runtime

// coresInitGC sets up how the GC stops the cores, on the first core.
func coresInitGC() {
	tinygo_cores_init(&coreThreads[0])
}

// coresRegisterGC runs first on every secondary core thread.
func coresRegisterGC(cpu uint32) {}
