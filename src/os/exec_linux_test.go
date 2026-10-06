//go:build linux && !baremetal && !tinygo.wasm

package os_test

import (
	"errors"
	"io"
	. "os"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// Test the functionality of the forkExec function, which is used to fork and exec a new process.
// This test is not run on Windows, as forkExec is not supported on Windows.
// This test is not run on Plan 9, as forkExec is not supported on Plan 9.
func TestForkExec(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Logf("skipping test on %s", runtime.GOOS)
		return
	}

	proc, err := StartProcess("/bin/echo", []string{"hello", "world"}, &ProcAttr{})
	if !errors.Is(err, nil) {
		t.Fatalf("forkExec failed: %v", err)
	}

	if proc == nil {
		t.Fatalf("proc is nil")
	}

	if proc.Pid == 0 {
		t.Fatalf("forkExec failed: new process has pid 0")
	}
	proc.Wait()
}

func TestForkExecErrNotExist(t *testing.T) {
	proc, err := StartProcess("invalid", []string{"invalid"}, &ProcAttr{})
	if !errors.Is(err, ErrNotExist) {
		t.Fatalf("wanted ErrNotExist, got %s\n", err)
	}

	if proc != nil {
		t.Fatalf("wanted nil, got %v\n", proc)
	}
}

func TestForkExecWait(t *testing.T) {
	proc, err := StartProcess("/bin/sh", []string{"sh", "-c", "exit 3"}, &ProcAttr{})
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	state, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !state.Exited() || state.Success() || state.ExitCode() != 3 {
		t.Fatalf("wanted exit status 3, got %v", state)
	}
	if state.String() != "exit status 3" {
		t.Fatalf("wanted \"exit status 3\", got %q", state.String())
	}
	if err := proc.Signal(Kill); !errors.Is(err, ErrProcessDone) {
		t.Fatalf("Signal after Wait: wanted ErrProcessDone, got %v", err)
	}
}

func TestForkExecProcDir(t *testing.T) {
	proc, err := StartProcess("/bin/echo", []string{"hello", "world"}, &ProcAttr{Dir: "dir-does-not-exist"})
	if !errors.Is(err, ErrNotExist) {
		t.Fatalf("wanted ErrNotExist, got %v\n", err)
	}
	if proc != nil {
		t.Fatalf("wanted nil, got %v\n", proc)
	}

	dir := t.TempDir()
	r, w, err := Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	proc, err = StartProcess("/bin/pwd", []string{"pwd"}, &ProcAttr{Dir: dir, Files: []*File{nil, w, w}})
	w.Close()
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	out, _ := io.ReadAll(r)
	if _, err := proc.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != dir {
		t.Fatalf("wanted %q, got %q", dir, got)
	}
}

func TestForkExecProcSys(t *testing.T) {
	_, err := StartProcess("/bin/echo", []string{"echo"}, &ProcAttr{Sys: &syscall.SysProcAttr{Chroot: "/"}})
	if err == nil {
		t.Fatalf("wanted an error for an unsupported SysProcAttr field")
	}

	proc, err := StartProcess("/bin/sh", []string{"sh", "-c", "exit 0"}, &ProcAttr{Sys: &syscall.SysProcAttr{Setsid: true}})
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	if state, err := proc.Wait(); err != nil || !state.Success() {
		t.Fatalf("Wait: %v %v", state, err)
	}
}

func TestForkExecKill(t *testing.T) {
	proc, err := StartProcess("/bin/sleep", []string{"sleep", "30"}, &ProcAttr{})
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	if err := proc.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	state, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	ws := state.Sys().(syscall.WaitStatus)
	if !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("wanted SIGKILL, got %v", state)
	}
}
