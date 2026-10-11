package pprof

import (
	"bufio"
	"bytes"
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

// symbolize names addresses from the executable's ELF symbol table. It streams
// the table and keeps only the symbols that cover pcs, because loading every
// symbol of a large binary costs tens of megabytes per profile.
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
	symtab := f.SectionByType(elf.SHT_SYMTAB)
	if symtab == nil || int(symtab.Link) >= len(f.Sections) {
		return out
	}
	strtab := f.Sections[symtab.Link]
	bias := uint64(0)
	if f.Type == elf.ET_DYN {
		bias = loadBias(exe)
	}
	addrs := make([]uint64, len(pcs))
	for i, pc := range pcs {
		addrs[i] = uint64(pc) - bias
	}
	sort.Slice(addrs, func(i, j int) bool { return addrs[i] < addrs[j] })
	nameOff := make(map[uint64]uint32, len(addrs))

	size := elf.Sym64Size
	if f.Class == elf.ELFCLASS32 {
		size = elf.Sym32Size
	}
	r := bufio.NewReaderSize(symtab.Open(), 64<<10)
	ent := make([]byte, size)
	for {
		if _, err := io.ReadFull(r, ent); err != nil {
			break
		}
		var name uint32
		var info byte
		var value, sz uint64
		if f.Class == elf.ELFCLASS32 {
			name = f.ByteOrder.Uint32(ent[0:])
			value = uint64(f.ByteOrder.Uint32(ent[4:]))
			sz = uint64(f.ByteOrder.Uint32(ent[8:]))
			info = ent[12]
		} else {
			name = f.ByteOrder.Uint32(ent[0:])
			info = ent[4]
			value = f.ByteOrder.Uint64(ent[8:])
			sz = f.ByteOrder.Uint64(ent[16:])
		}
		if elf.ST_TYPE(info) != elf.STT_FUNC || value == 0 || sz == 0 {
			continue
		}
		i := sort.Search(len(addrs), func(i int) bool { return addrs[i] >= value })
		for ; i < len(addrs) && addrs[i] < value+sz; i++ {
			nameOff[addrs[i]] = name
		}
	}
	buf := make([]byte, 512)
	for _, pc := range pcs {
		off, ok := nameOff[uint64(pc)-bias]
		if !ok {
			continue
		}
		n, _ := strtab.ReadAt(buf, int64(off))
		name := buf[:n]
		if j := bytes.IndexByte(name, 0); j >= 0 {
			name = name[:j]
		}
		out[pc] = string(name)
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
