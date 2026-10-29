//go:build linux && !baremetal && !nintendoswitch && !wasip1 && !wasm_unknown && !wasip2 && !(scheduler.tasks || scheduler.cores)

package runtime

import "internal/task"

// Goroutine stacks are thread stacks here, found through the thread itself.
func taskStackBounds() (lo, hi uintptr) { return 0, 0 }

func printOtherGoroutines(self *task.Task) {
	crashStr("\nother goroutines not shown: this scheduler does not track their stacks\n")
}
