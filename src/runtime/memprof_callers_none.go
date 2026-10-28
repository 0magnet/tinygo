//go:build gc.boehm && !((scheduler.tasks || scheduler.cores) && linux && !baremetal && (amd64 || arm64))

package runtime

func memProfCallers(pc uintptr, pcs *[memProfDepth]uintptr) {
	pcs[0] = pc
}
