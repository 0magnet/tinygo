// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && !baremetal && !tinygo.wasm && !nintendoswitch

package os

import (
	"errors"
	"internal/itoa"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// The only signal values guaranteed to be present in the os package on all
// systems are os.Interrupt (send the process an interrupt) and os.Kill (force
// the process to exit). On Windows, sending os.Interrupt to a process with
// os.Process.Signal is not implemented; it will return an error instead of
// sending a signal.
var (
	Interrupt Signal = syscall.SIGINT
	Kill      Signal = syscall.SIGKILL
)

// openCloexec is added to the flags of every file opened by OpenFile, so a
// child process only inherits the files it is given.
const openCloexec = syscall.O_CLOEXEC

// processHandle tracks whether a started process has been reaped, so that
// Signal never reaches a recycled pid.
type processHandle struct {
	sigMu sync.RWMutex
	done  bool
}

// Keep compatible with golang and always succeed and return new proc with pid on Linux.
func findProcess(pid int) (*Process, error) {
	return &Process{Pid: pid}, nil
}

func (p *Process) release() error {
	p.Pid = -1
	runtime.SetFinalizer(p, nil)
	return nil
}

// execAttr must match struct tinygo_exec_attr in exec_linux.c.
type execAttr struct {
	path      *byte
	argv      **byte
	envp      **byte
	dir       *byte
	fds       *int32
	groups    *uint32
	nfds      int32
	ngroups   int32
	flags     int32
	pgid      int32
	ctty      int32
	pdeathsig int32
	uid       uint32
	gid       uint32
	err       int32
}

const (
	execSetsid     = 1
	execSetpgid    = 2
	execSetctty    = 4
	execNoctty     = 8
	execCredential = 16
	execSetgroups  = 32
)

//go:linkname tinygo_exec_start tinygo_exec_start
func tinygo_exec_start(attr *execAttr) int32

// forkExec starts argv0 in a new process. The child side runs entirely in C
// (exec_linux.c) so that no Go code runs between clone and execve.
func forkExec(argv0 string, argv []string, attr *ProcAttr) (pid int, err error) {
	if len(argv) == 0 {
		return 0, errors.New("exec: no argv")
	}
	if attr == nil {
		attr = new(ProcAttr)
	}

	var a execAttr
	path, err := syscall.BytePtrFromString(argv0)
	if err != nil {
		return 0, err
	}
	a.path = path
	argvp, err := syscall.SlicePtrFromStrings(argv)
	if err != nil {
		return 0, err
	}
	a.argv = &argvp[0]
	env := attr.Env
	if env == nil {
		env = Environ()
	}
	envp, err := syscall.SlicePtrFromStrings(env)
	if err != nil {
		return 0, err
	}
	a.envp = &envp[0]
	if attr.Dir != "" {
		a.dir, err = syscall.BytePtrFromString(attr.Dir)
		if err != nil {
			return 0, err
		}
	}

	fds := make([]int32, len(attr.Files)+1)
	for i, f := range attr.Files {
		fds[i] = -1
		if f != nil {
			fds[i] = int32(f.Fd())
		}
	}
	a.fds = &fds[0]
	a.nfds = int32(len(attr.Files))

	var groups []uint32
	if sys := attr.Sys; sys != nil {
		if err := checkSysProcAttr(sys); err != nil {
			return 0, err
		}
		if sys.Setsid {
			a.flags |= execSetsid
		}
		if sys.Setpgid {
			a.flags |= execSetpgid
			a.pgid = int32(sys.Pgid)
		}
		if sys.Setctty {
			a.flags |= execSetctty
			a.ctty = int32(sys.Ctty)
		}
		if sys.Noctty {
			a.flags |= execNoctty
		}
		a.pdeathsig = int32(sys.Pdeathsig)
		if cred := sys.Credential; cred != nil {
			a.flags |= execCredential
			a.uid = cred.Uid
			a.gid = cred.Gid
			if !cred.NoSetGroups {
				a.flags |= execSetgroups
				groups = append(groups, cred.Groups...)
				a.ngroups = int32(len(groups))
				groups = append(groups, 0)
				a.groups = &groups[0]
			}
		}
	}

	p := tinygo_exec_start(&a)
	runtime.KeepAlive(path)
	runtime.KeepAlive(argvp)
	runtime.KeepAlive(envp)
	runtime.KeepAlive(fds)
	runtime.KeepAlive(groups)
	runtime.KeepAlive(attr)
	if p < 0 {
		return 0, syscall.Errno(a.err)
	}
	return int(p), nil
}

// checkSysProcAttr rejects the SysProcAttr fields that are not implemented,
// rather than starting a process without them.
func checkSysProcAttr(sys *syscall.SysProcAttr) error {
	switch {
	case sys.Chroot != "":
		return errors.New("os: SysProcAttr.Chroot is not supported")
	case sys.Ptrace:
		return errors.New("os: SysProcAttr.Ptrace is not supported")
	case sys.Foreground:
		return errors.New("os: SysProcAttr.Foreground is not supported")
	case sys.Cloneflags != 0:
		return errors.New("os: SysProcAttr.Cloneflags is not supported")
	case sys.Unshareflags != 0:
		return errors.New("os: SysProcAttr.Unshareflags is not supported")
	case len(sys.UidMappings) != 0 || len(sys.GidMappings) != 0:
		return errors.New("os: SysProcAttr user namespace mappings are not supported")
	case len(sys.AmbientCaps) != 0:
		return errors.New("os: SysProcAttr.AmbientCaps is not supported")
	case sys.UseCgroupFD:
		return errors.New("os: SysProcAttr.UseCgroupFD is not supported")
	case sys.PidFD != nil:
		return errors.New("os: SysProcAttr.PidFD is not supported")
	}
	return nil
}

func startProcess(name string, argv []string, attr *ProcAttr) (p *Process, err error) {
	if attr != nil && attr.Dir != "" {
		if _, err := Stat(attr.Dir); err != nil {
			pe := err.(*PathError)
			pe.Op = "chdir"
			return nil, pe
		}
	}

	pid, err := forkExec(name, argv, attr)
	if err != nil {
		return nil, &PathError{Op: "fork/exec", Path: name, Err: err}
	}
	return &Process{Pid: pid, h: new(processHandle)}, nil
}

func (p *Process) wait() (*ProcessState, error) {
	if h := p.h; h != nil {
		// Wait until the child is waitable without reaping it, then mark it
		// done before its pid can be reused, as Go's os does.
		var info [128]byte
		err := ignoringEINTR(func() error {
			_, _, e := syscall.Syscall6(syscall.SYS_WAITID, 1 /* P_PID */, uintptr(p.Pid),
				uintptr(unsafe.Pointer(&info[0])), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
			if e != 0 {
				return e
			}
			return nil
		})
		if err == nil {
			h.sigMu.Lock()
			h.done = true
			h.sigMu.Unlock()
		}
	}

	var status syscall.WaitStatus
	var rusage syscall.Rusage
	var pid1 int
	err := ignoringEINTR(func() error {
		var err error
		pid1, err = syscall.Wait4(p.Pid, &status, 0, &rusage)
		return err
	})
	if err != nil {
		return nil, NewSyscallError("wait", err)
	}
	if h := p.h; h != nil {
		h.sigMu.Lock()
		h.done = true
		h.sigMu.Unlock()
	}
	return &ProcessState{pid: pid1, status: status, rusage: &rusage}, nil
}

func (p *Process) signal(sig Signal) error {
	s, ok := sig.(syscall.Signal)
	if !ok {
		return errors.New("os: unsupported signal type")
	}
	if p.Pid == -1 {
		return errProcessReleased
	}
	if p.Pid == 0 {
		return errors.New("os: process not initialized")
	}
	if h := p.h; h != nil {
		h.sigMu.RLock()
		defer h.sigMu.RUnlock()
		if h.done {
			return ErrProcessDone
		}
	}
	err := syscall.Kill(p.Pid, s)
	if err == syscall.ESRCH {
		return ErrProcessDone
	}
	if err != nil {
		return NewSyscallError("kill", err)
	}
	return nil
}

// ProcessState stores information about a process, as reported by Wait.
type ProcessState struct {
	pid    int
	status syscall.WaitStatus
	rusage *syscall.Rusage
}

// Pid returns the process id of the exited process.
func (p *ProcessState) Pid() int {
	return p.pid
}

// Exited reports whether the program has exited.
func (p *ProcessState) Exited() bool {
	return p.status.Exited()
}

// Success reports whether the program exited successfully.
func (p *ProcessState) Success() bool {
	return p.status.ExitStatus() == 0
}

// Sys returns the syscall.WaitStatus of the process.
func (p *ProcessState) Sys() interface{} {
	return p.status
}

// SysUsage returns the *syscall.Rusage of the exited process.
func (p *ProcessState) SysUsage() interface{} {
	return p.rusage
}

// UserTime returns the user CPU time of the exited process and its children.
func (p *ProcessState) UserTime() time.Duration {
	return time.Duration(p.rusage.Utime.Nano()) * time.Nanosecond
}

// SystemTime returns the system CPU time of the exited process and its children.
func (p *ProcessState) SystemTime() time.Duration {
	return time.Duration(p.rusage.Stime.Nano()) * time.Nanosecond
}

// ExitCode returns the exit code of the exited process, or -1
// if the process hasn't exited or was terminated by a signal.
func (p *ProcessState) ExitCode() int {
	if p == nil {
		return -1
	}
	return p.status.ExitStatus()
}

func (p *ProcessState) String() string {
	if p == nil {
		return "<nil>"
	}
	status := p.status
	res := ""
	switch {
	case status.Exited():
		res = "exit status " + itoa.Itoa(status.ExitStatus())
	case status.Signaled():
		res = "signal: " + status.Signal().String()
	case status.Stopped():
		res = "stop signal: " + status.StopSignal().String()
		if status.StopSignal() == syscall.SIGTRAP && status.TrapCause() != 0 {
			res += " (trap " + itoa.Itoa(status.TrapCause()) + ")"
		}
	case status.Continued():
		res = "continued"
	}
	if status.CoreDump() {
		res += " (core dumped)"
	}
	return res
}
