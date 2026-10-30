//go:build !(linux && (arm || arm64) && !baremetal && !wasip1 && !wasip2 && !wasm_unknown && !nintendoswitch)

package runtime

func initHWCap() {}
