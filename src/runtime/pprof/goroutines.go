package pprof

import (
	"bufio"
	"debug/elf"
	"fmt"
	"internal/task"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// writeGoroutines groups live goroutines by the function they started with,
// which is as close to a stack as TinyGo can get without unwinding.
func writeGoroutines(w io.Writer) error {
	n := task.GoroutineStartPCs(nil)
	buf := make([]uintptr, n+64)
	n = task.GoroutineStartPCs(buf)
	if n > len(buf) {
		n = len(buf)
	}
	counts := map[uintptr]int{}
	for _, pc := range buf[:n] {
		counts[pc]++
	}
	pcs := make([]uintptr, 0, len(counts))
	for pc := range counts {
		pcs = append(pcs, pc)
	}
	sort.Slice(pcs, func(i, j int) bool { return counts[pcs[i]] > counts[pcs[j]] })
	names := symbolize(pcs)
	if _, err := fmt.Fprintf(w, "goroutine profile: total %d\n# grouped by the function each goroutine started with; TinyGo has no stacks\n\n", n); err != nil {
		return err
	}
	for _, pc := range pcs {
		name := names[pc]
		if pc == 0 {
			name = "main goroutine"
		}
		if _, err := fmt.Fprintf(w, "%d @ %#x\n#\t%#x\t%s\n\n", counts[pc], pc, pc, name); err != nil {
			return err
		}
	}
	return nil
}

// symbolize names addresses from the executable's ELF symbol table.
func symbolize(pcs []uintptr) map[uintptr]string {
	out := map[uintptr]string{}
	exe, err := os.Executable()
	if err != nil {
		return out
	}
	f, err := elf.Open(exe)
	if err != nil {
		return out
	}
	defer f.Close()
	syms, err := f.Symbols()
	if err != nil {
		return out
	}
	bias := uint64(0)
	if f.Type == elf.ET_DYN {
		bias = loadBias(exe)
	}
	funcs := syms[:0]
	for _, s := range syms {
		if elf.ST_TYPE(s.Info) == elf.STT_FUNC && s.Value != 0 {
			funcs = append(funcs, s)
		}
	}
	sort.Slice(funcs, func(i, j int) bool { return funcs[i].Value < funcs[j].Value })
	for _, pc := range pcs {
		a := uint64(pc) - bias
		i := sort.Search(len(funcs), func(i int) bool { return funcs[i].Value > a }) - 1
		if i >= 0 && (funcs[i].Size == 0 || a < funcs[i].Value+funcs[i].Size) {
			out[pc] = funcs[i].Name
		}
	}
	return out
}

// loadBias finds where a position independent executable was mapped.
func loadBias(exe string) uint64 {
	f, err := os.Open("/proc/self/maps")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 6 && fields[5] == exe && fields[2] == "00000000" {
			start, _, _ := strings.Cut(fields[0], "-")
			v, err := strconv.ParseUint(start, 16, 64)
			if err == nil {
				return v
			}
		}
	}
	return 0
}
