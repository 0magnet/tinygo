//go:build scheduler.tasks && linux && !baremetal && arm64

package task

func savedFP(r *calleeSavedRegs) uintptr { return r.x29 }
