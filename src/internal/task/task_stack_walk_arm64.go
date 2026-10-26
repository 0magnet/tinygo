//go:build (scheduler.tasks || scheduler.cores) && linux && !baremetal && arm64

package task

func savedFP(r *calleeSavedRegs) uintptr { return r.x29 }
