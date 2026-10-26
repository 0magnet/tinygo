//go:build (scheduler.tasks || scheduler.cores) && linux && !baremetal && amd64

package task

func savedFP(r *calleeSavedRegs) uintptr { return r.rbp }
