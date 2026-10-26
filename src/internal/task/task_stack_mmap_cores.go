//go:build scheduler.cores && linux && !baremetal

package task

// Another core could reuse the stack of an exiting task before the task has
// switched off it, so the stack is released after Resume returns.
const stackReleasedOnResume = true
