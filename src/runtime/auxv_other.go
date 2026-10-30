//go:build !linux || baremetal || wasip1 || wasip2 || wasm_unknown || nintendoswitch

package runtime

// getAuxv is linknamed from golang.org/x/sys/cpu.
func getAuxv() []uintptr {
	return nil
}
