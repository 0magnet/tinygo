//go:build linux && scheduler.tasks && !baremetal && !nintendoswitch && !amd64 && !386

package runtime

const epollPad = 4
