//go:build !(scheduler.asyncify && goroutinelist)

package task

type listLinks struct{}

func listTask(t *Task) {}

func unlistTask(t *Task) {}
