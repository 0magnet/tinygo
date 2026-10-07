package pprof

// TinyGo has no stack unwinder, so profiles carry counts and memory statistics
// but no stacks. The text form (debug >= 1) is what they serve.

import (
	"errors"
	"fmt"
	"io"
	"runtime"
)

var ErrUnimplemented = errors.New("runtime/pprof: unimplemented")

var errNoCPUProfile = errors.New("runtime/pprof: CPU profiling needs stack unwinding, which TinyGo does not have")

// Profile is a named profile.
type Profile struct {
	name string
}

var profiles = []*Profile{
	{name: "goroutine"},
	{name: "threadcreate"},
	{name: "heap"},
	{name: "allocs"},
	{name: "block"},
	{name: "mutex"},
}

func StartCPUProfile(w io.Writer) error {
	return errNoCPUProfile
}

func StopCPUProfile() {
}

func WriteHeapProfile(w io.Writer) error {
	return Lookup("heap").WriteTo(w, 1)
}

func Lookup(name string) *Profile {
	for _, p := range profiles {
		if p.name == name {
			return p
		}
	}
	return nil
}

func (p *Profile) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

func (p *Profile) Count() int {
	if p == nil {
		return 0
	}
	switch p.name {
	case "goroutine", "threadcreate":
		return runtime.NumGoroutine()
	}
	return 0
}

func (p *Profile) WriteTo(w io.Writer, debug int) error {
	if p == nil {
		return ErrUnimplemented
	}
	switch p.name {
	case "goroutine":
		return writeGoroutines(w)
	case "threadcreate":
		_, err := fmt.Fprintf(w, "%s profile: total %d\n# stacks are not available in TinyGo builds\n", p.name, p.Count())
		return err
	case "heap", "allocs":
		return writeMemStats(w, p.name)
	}
	_, err := fmt.Fprintf(w, "--- %s:\n# no samples in TinyGo builds\n", p.name)
	return err
}

func writeMemStats(w io.Writer, name string) error {
	var s runtime.MemStats
	runtime.ReadMemStats(&s)
	_, err := fmt.Fprintf(w, "%s profile: 0: 0 [0: 0] @ heap/1\n# stacks are not available in TinyGo builds\n\n"+
		"# runtime.MemStats\n# Alloc = %d\n# TotalAlloc = %d\n# Sys = %d\n# Mallocs = %d\n# Frees = %d\n"+
		"# HeapAlloc = %d\n# HeapSys = %d\n# HeapIdle = %d\n# HeapInuse = %d\n# HeapReleased = %d\n"+
		"# NumGC = %d\n# NumGoroutine = %d\n",
		name, s.Alloc, s.TotalAlloc, s.Sys, s.Mallocs, s.Frees,
		s.HeapAlloc, s.HeapSys, s.HeapIdle, s.HeapInuse, s.HeapReleased,
		s.NumGC, runtime.NumGoroutine())
	if err != nil {
		return err
	}
	return writeHeapSites(w)
}

func Profiles() []*Profile {
	return append([]*Profile(nil), profiles...)
}
