//go:build (scheduler.tasks || scheduler.asyncify) && !(linux && scheduler.tasks && !baremetal && !nintendoswitch)

package runtime

func schedulerPollTick() {}
