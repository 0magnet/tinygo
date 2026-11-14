//go:build linux && !baremetal && !tinygo.wasm

package os_test

import (
	"errors"
	"io"
	. "os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Test the functionality of the forkExec function, which is used to fork and exec a new process.
// This test is not run on Windows, as forkExec is not supported on Windows.
// This test is not run on Plan 9, as forkExec is not supported on Plan 9.
func TestForkExec(t *testing.T) {
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

// The caller keeps running and the command runs once.
func TestForkExecParentContinues(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	proc, err := StartProcess("/bin/sh", []string{"sh", "-c", "echo child >> " + out}, &ProcAttr{})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := proc.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if !ps.Success() {
		t.Fatalf("child did not exit cleanly: %v", ps)
	}
	data, err := ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "child\n" {
		t.Fatalf("child output = %q, want %q", data, "child\n")
	}
}

func TestProcessWait(t *testing.T) {
	for _, tc := range []struct {
		script  string
		code    int
		success bool
		str     string
	}{
		{"exit 0", 0, true, "exit status 0"},
		{"exit 3", 3, false, "exit status 3"},
	} {
		proc, err := StartProcess("/bin/sh", []string{"sh", "-c", tc.script}, &ProcAttr{})
		if err != nil {
			t.Fatal(err)
		}
		ps, err := proc.Wait()
		if err != nil {
			t.Fatal(err)
		}
		if ps.Pid() != proc.Pid {
			t.Errorf("%s: Pid() = %d, want %d", tc.script, ps.Pid(), proc.Pid)
		}
		if !ps.Exited() || ps.ExitCode() != tc.code || ps.Success() != tc.success || ps.String() != tc.str {
			t.Errorf("%s: Exited %v, ExitCode %d, Success %v, String %q", tc.script, ps.Exited(), ps.ExitCode(), ps.Success(), ps.String())
		}
	}
}

func TestProcessWaitSignal(t *testing.T) {
	proc, err := StartProcess("/bin/sh", []string{"sh", "-c", "kill -KILL $$"}, &ProcAttr{})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := proc.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if ps.Exited() || ps.ExitCode() != -1 || ps.Success() || ps.String() != "signal: killed" {
		t.Errorf("Exited %v, ExitCode %d, Success %v, String %q", ps.Exited(), ps.ExitCode(), ps.Success(), ps.String())
	}
}

// Wait on a zero Process must not reap another child.
func TestProcessWaitZeroValue(t *testing.T) {
	proc, err := StartProcess("/bin/sh", []string{"sh", "-c", "exit 7"}, &ProcAttr{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := new(Process).Wait(); err == nil {
		t.Fatal("wanted an error from Wait on a zero Process")
	}
	ps, err := proc.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if ps.ExitCode() != 7 {
		t.Fatalf("ExitCode = %d, want 7", ps.ExitCode())
	}
}
