//go:build scheduler.asyncify && goroutinelist

package task

// listLinks chains every live task so the goroutine profile can walk them.
// The list keeps blocked tasks alive, so it is opt in for diagnosis.
type listLinks struct {
	listNext, listPrev *Task
}

var listHead *Task

func listTask(t *Task) {
	t.state.listNext = listHead
	if listHead != nil {
		listHead.state.listPrev = t
	}
	listHead = t
}

func unlistTask(t *Task) {
	s := &t.state
	if s.listPrev != nil {
		s.listPrev.state.listNext = s.listNext
	} else if listHead == t {
		listHead = s.listNext
	}
	if s.listNext != nil {
		s.listNext.state.listPrev = s.listPrev
	}
	s.listNext, s.listPrev = nil, nil
}

// GoroutineStacks reports each live goroutine with its entry function as
// the only frame, since asyncify stacks cannot be walked.
func GoroutineStacks(fn func(reason WaitReason, pcs []uintptr)) bool {
	var pc [1]uintptr
	for t := listHead; t != nil; t = t.state.listNext {
		r := t.WaitReason
		if t == currentTask {
			r = WaitRunning
		}
		pc[0] = t.state.entry
		fn(r, pc[:])
	}
	return true
}

// MappedStackTop returns zero: the GC finds the stack bounds without the task.
func MappedStackTop() uintptr { return 0 }

// MarkParkedStacks does nothing: no stacks live outside the heap here.
func MarkParkedStacks(mark func(start, end uintptr)) {}
