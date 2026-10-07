package pprof

import (
	"fmt"
	"io"
	"sort"
	_ "unsafe"
)

//go:linkname readMemProfile runtime.readMemProfile
func readMemProfile(pcs, inuse, alloc []uintptr) int

// writeHeapSites lists the sampled allocation sites. A site is the function
// that called the allocator, which is a runtime helper for append, maps and
// string building.
func writeHeapSites(w io.Writer) error {
	n := readMemProfile(nil, nil, nil)
	if n == 0 {
		return nil
	}
	pcs, inuse, alloc := make([]uintptr, n), make([]uintptr, n), make([]uintptr, n)
	readMemProfile(pcs, inuse, alloc)
	idx := make([]int, 0, n)
	for i, pc := range pcs {
		if pc != 0 {
			idx = append(idx, i)
		}
	}
	keys := make([]uintptr, len(idx))
	for k, i := range idx {
		keys[k] = pcs[i]
	}
	names := symbolize(keys)
	var totIn, totAlloc uintptr
	for _, i := range idx {
		totIn += inuse[i]
		totAlloc += alloc[i]
	}
	if _, err := fmt.Fprintf(w, "\n# sampled allocation sites, one sample per ~512 KiB allocated\n# inuse_bytes alloc_bytes site (sampled totals %d inuse, %d alloc)\n", totIn, totAlloc); err != nil {
		return err
	}
	sort.Slice(idx, func(a, b int) bool {
		if inuse[idx[a]] != inuse[idx[b]] {
			return inuse[idx[a]] > inuse[idx[b]]
		}
		return alloc[idx[a]] > alloc[idx[b]]
	})
	for _, i := range idx {
		name := names[pcs[i]]
		if name == "" {
			name = fmt.Sprintf("%#x", pcs[i])
		}
		if _, err := fmt.Fprintf(w, "%d %d %s\n", inuse[i], alloc[i], name); err != nil {
			return err
		}
	}
	return nil
}
