//go:build !(scheduler.tasks || scheduler.cores)

package task

// GoroutineStacks reports false: stacks are not tracked here.
func GoroutineStacks(fn func(pcs []uintptr)) bool { return false }

// MappedStackTop returns zero: the GC finds the stack bounds without the task.
func MappedStackTop() uintptr { return 0 }

// MarkParkedStacks does nothing: no stacks live outside the heap here.
func MarkParkedStacks(mark func(start, end uintptr)) {}
