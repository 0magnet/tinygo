//go:build linux && (scheduler.tasks || scheduler.cores) && !baremetal && !nintendoswitch && (amd64 || 386)

package runtime

const epollPad = 0
