//go:build !((scheduler.tasks || scheduler.cores) && linux && !baremetal && !wasm_unknown && !nintendoswitch && !wasip1 && !wasip2 && !tinygo.wasm)

package os

// makePollable does nothing: here a blocking read stops only its goroutine's
// thread, or there is no poller to park in.
func (f *file) makePollable() {}

func (f *file) setBlocking() {}
