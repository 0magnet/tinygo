//go:build (scheduler.tasks || scheduler.cores) && !(scheduler.tasks && linux && !baremetal)

package task

import (
	"internal/gclayout"
	"unsafe"
)

const hasReleasableStack = false

// stackInfo is empty: the stack is a heap object found through canaryPtr.
type stackInfo struct{}

func allocStack(s *state, size uintptr) unsafe.Pointer {
	return runtime_alloc(size, gclayout.Conservative.AsPtr())
}

func linkTask(t *Task) {}

// unlinkStack does nothing here, since only asyncify keeps a list of stacks.
func unlinkStack(t *Task) {}

// StackTop returns zero: the GC finds the stack bounds from the heap.
func StackTop() uintptr { return 0 }

// MarkParkedStacks does nothing: parked stacks are heap objects.
func MarkParkedStacks(mark func(start, end uintptr)) {}

func canaryFor(stack unsafe.Pointer) *uintptr { return (*uintptr)(stack) }
