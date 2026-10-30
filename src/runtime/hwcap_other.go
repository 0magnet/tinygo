//go:build !(linux && (arm || arm64) && !baremetal && !nintendoswitch)

package runtime

func initHWCap() {}
