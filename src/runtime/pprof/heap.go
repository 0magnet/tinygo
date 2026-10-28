package pprof

import (
	"fmt"
	"io"
	"sort"
	"strings"
	_ "unsafe"
)

//go:linkname readMemProfile runtime.readMemProfile
func readMemProfile(pcs, inuse, alloc []uintptr) int

//go:linkname readMemProfileDepth runtime.readMemProfileDepth
func readMemProfileDepth() int

type heapSite struct {
	pcs          []uintptr
	inuse, alloc uintptr
}

// writeHeapSites lists the sampled allocation sites, first by the function
// that called the allocator, which is often a runtime helper for append, maps
// and string building, then by call stack.
func writeHeapSites(w io.Writer) error {
	n := readMemProfile(nil, nil, nil)
	if n == 0 {
		return nil
	}
	depth := readMemProfileDepth()
	pcs, inuse, alloc := make([]uintptr, n*depth), make([]uintptr, n), make([]uintptr, n)
	readMemProfile(pcs, inuse, alloc)
	var stacks []heapSite
	byPC := map[uintptr]*heapSite{}
	var keys []uintptr
	seen := map[uintptr]bool{}
	var totIn, totAlloc uintptr
	for i := 0; i < n; i++ {
		s := pcs[i*depth : (i+1)*depth]
		if s[0] == 0 {
			continue
		}
		for len(s) > 0 && s[len(s)-1] == 0 {
			s = s[:len(s)-1]
		}
		stacks = append(stacks, heapSite{s, inuse[i], alloc[i]})
		totIn += inuse[i]
		totAlloc += alloc[i]
		f := byPC[s[0]]
		if f == nil {
			f = &heapSite{pcs: s[:1]}
			byPC[s[0]] = f
		}
		f.inuse += inuse[i]
		f.alloc += alloc[i]
		for _, pc := range s {
			if !seen[pc] {
				seen[pc] = true
				keys = append(keys, pc)
			}
		}
	}
	names := symbolize(keys)
	name := func(pc uintptr) string {
		if s := names[pc]; s != "" {
			return s
		}
		return fmt.Sprintf("%#x", pc)
	}
	sites := make([]heapSite, 0, len(byPC))
	for _, f := range byPC {
		sites = append(sites, *f)
	}
	if _, err := fmt.Fprintf(w, "\n# sampled allocation sites, one sample per ~512 KiB allocated\n# inuse_bytes alloc_bytes site (sampled totals %d inuse, %d alloc)\n", totIn, totAlloc); err != nil {
		return err
	}
	if err := writeSites(w, sites, func(s heapSite) string { return name(s.pcs[0]) }); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\n# sampled allocation stacks, innermost first\n# inuse_bytes alloc_bytes site<caller<caller...\n"); err != nil {
		return err
	}
	return writeSites(w, stacks, func(s heapSite) string {
		parts := make([]string, len(s.pcs))
		for i, pc := range s.pcs {
			parts[i] = name(pc)
		}
		return strings.Join(parts, "<")
	})
}

func writeSites(w io.Writer, sites []heapSite, label func(heapSite) string) error {
	sort.Slice(sites, func(a, b int) bool {
		if sites[a].inuse != sites[b].inuse {
			return sites[a].inuse > sites[b].inuse
		}
		return sites[a].alloc > sites[b].alloc
	})
	for _, s := range sites {
		if _, err := fmt.Fprintf(w, "%d %d %s\n", s.inuse, s.alloc, label(s)); err != nil {
			return err
		}
	}
	return nil
}
