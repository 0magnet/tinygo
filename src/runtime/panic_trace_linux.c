//go:build none

// This file is included on Linux (despite the //go:build line above). It
// walks and names the stack of a crashing goroutine without allocating, so it
// works with the GC lock held and in -no-debug builds, which keep .symtab.

#define _GNU_SOURCE
#include <elf.h>
#include <fcntl.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <sys/auxv.h>
#include <unistd.h>

#if UINTPTR_MAX == 0xffffffffu
typedef Elf32_Ehdr Ehdr;
typedef Elf32_Shdr Shdr;
typedef Elf32_Sym Sym;
#define SYM_TYPE ELF32_ST_TYPE
#define SYM_BIND ELF32_ST_BIND
#define NATIVE_CLASS ELFCLASS32
#else
typedef Elf64_Ehdr Ehdr;
typedef Elf64_Shdr Shdr;
typedef Elf64_Sym Sym;
#define SYM_TYPE ELF64_ST_TYPE
#define SYM_BIND ELF64_ST_BIND
#define NATIVE_CLASS ELFCLASS64
#endif

// tinygo_crash_walk stores up to n return addresses by following frame
// pointers from fp, or from its own frame if fp is zero. Every frame read
// must lie in [lo, hi), so a corrupt chain stops the walk instead of faulting.
__attribute__((noinline))
size_t tinygo_crash_walk(uintptr_t *pcs, size_t n, uintptr_t fp, uintptr_t lo, uintptr_t hi) {
    if (fp == 0) {
        fp = (uintptr_t)__builtin_frame_address(0);
    }
    size_t i = 0;
    while (i < n && fp >= lo && fp % sizeof(uintptr_t) == 0 && fp + 2 * sizeof(uintptr_t) <= hi) {
        uintptr_t next = ((uintptr_t *)fp)[0];
        uintptr_t pc = ((uintptr_t *)fp)[1];
        if (pc == 0) {
            break;
        }
        pcs[i++] = pc;
        if (next <= fp) {
            break;
        }
        fp = next;
    }
    return i;
}

#define UNSIZED_REACH 512
#define WEAK_NAME 0x80000000u

static int exe_fd = -1;
static off_t str_off;
static size_t str_size;
static Sym sym_buf[512];

static int read_at(void *buf, size_t n, off_t off) {
    char *p = buf;
    while (n > 0) {
        ssize_t r = pread(exe_fd, p, n, off);
        if (r <= 0) {
            return 0;
        }
        p += r;
        n -= r;
        off += r;
    }
    return 1;
}

// tinygo_crash_symbolize names the n ascending addresses in addrs from the
// executable's symbol table. It sets starts[i] to the start of the function
// that holds addrs[i] and names[i] to its name's offset in the string table,
// leaving starts[i] zero where no function covers it. It reports success.
int tinygo_crash_symbolize(const uintptr_t *addrs, size_t n, uintptr_t *starts, uint32_t *names) {
    for (size_t i = 0; i < n; i++) {
        starts[i] = 0;
    }
    if (exe_fd < 0) {
        exe_fd = open("/proc/self/exe", O_RDONLY | O_CLOEXEC);
        if (exe_fd < 0) {
            return 0;
        }
    }
    Ehdr eh;
    if (!read_at(&eh, sizeof(eh), 0) || eh.e_ident[EI_CLASS] != NATIVE_CLASS || eh.e_shentsize != sizeof(Shdr)) {
        return 0;
    }
    uintptr_t bias = 0;
    if (eh.e_type == ET_DYN) {
        bias = getauxval(AT_PHDR) - eh.e_phoff;
    }
    Shdr sym, str;
    int found = 0;
    for (unsigned i = 0; i < eh.e_shnum && !found; i++) {
        if (!read_at(&sym, sizeof(sym), eh.e_shoff + (off_t)i * sizeof(Shdr))) {
            return 0;
        }
        found = sym.sh_type == SHT_SYMTAB;
    }
    if (!found || sym.sh_link >= eh.e_shnum || !read_at(&str, sizeof(str), eh.e_shoff + (off_t)sym.sh_link * sizeof(Shdr))) {
        return 0;
    }
    str_off = str.sh_offset;
    str_size = str.sh_size;

    size_t count = sym.sh_size / sizeof(Sym);
    for (size_t done = 0; done < count;) {
        size_t chunk = count - done;
        if (chunk > sizeof(sym_buf) / sizeof(Sym)) {
            chunk = sizeof(sym_buf) / sizeof(Sym);
        }
        if (!read_at(sym_buf, chunk * sizeof(Sym), sym.sh_offset + (off_t)done * sizeof(Sym))) {
            return 0;
        }
        done += chunk;
        for (size_t k = 0; k < chunk; k++) {
            Sym *s = &sym_buf[k];
            // Local untyped symbols are mapping symbols such as $x on arm64.
            int type = SYM_TYPE(s->st_info);
            if ((type != STT_FUNC && type != STT_NOTYPE) || s->st_value == 0 ||
                s->st_shndx == SHN_UNDEF || s->st_shndx >= SHN_LORESERVE ||
                (type == STT_NOTYPE && (s->st_size != 0 || SYM_BIND(s->st_info) == STB_LOCAL))) {
                continue;
            }
            // Assembly routines such as tinygo_startTask have no size. They
            // name what follows them only where no sized function does.
            int weak = s->st_size == 0;
            uintptr_t lo = s->st_value + bias, hi = lo + (weak ? UNSIZED_REACH : s->st_size);
            size_t a = 0, b = n;
            while (a < b) {
                size_t m = a + (b - a) / 2;
                if (addrs[m] < lo) {
                    a = m + 1;
                } else {
                    b = m;
                }
            }
            for (; a < n && addrs[a] < hi; a++) {
                if (starts[a] == 0 || (names[a] & WEAK_NAME && (!weak || lo > starts[a]))) {
                    starts[a] = lo;
                    names[a] = s->st_name | (weak ? WEAK_NAME : 0);
                }
            }
        }
    }
    return 1;
}

// tinygo_crash_symname copies the name at offset off of the string table
// read by tinygo_crash_symbolize into buf and returns its length.
size_t tinygo_crash_symname(uint32_t off, char *buf, size_t len) {
    off &= ~WEAK_NAME;
    if (exe_fd < 0 || off >= str_size || len == 0) {
        return 0;
    }
    if (len > str_size - off) {
        len = str_size - off;
    }
    ssize_t r = pread(exe_fd, buf, len, str_off + off);
    if (r <= 0) {
        return 0;
    }
    for (ssize_t i = 0; i < r; i++) {
        if (buf[i] == 0) {
            return i;
        }
    }
    return r;
}

// tinygo_crash_all reports whether TINYGO_PANIC_ALL=1 asks for every
// goroutine's stack, as GOTRACEBACK=all does in Go.
int tinygo_crash_all(void) {
    const char *v = getenv("TINYGO_PANIC_ALL");
    return v != NULL && v[0] == '1' && v[1] == 0;
}
