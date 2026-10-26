//go:build scheduler.tasks && linux && !baremetal && amd64

package task

func savedFP(r *calleeSavedRegs) uintptr { return r.rbp }
