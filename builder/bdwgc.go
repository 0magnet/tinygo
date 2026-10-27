package builder

// The well-known conservative Boehm-Demers-Weiser GC.
// This file provides a way to compile this GC for use with TinyGo.

import (
	"path/filepath"
	"strings"

	"github.com/tinygo-org/tinygo/compileopts"
	"github.com/tinygo-org/tinygo/goenv"
)

var BoehmGC = Library{
	name: "bdwgc",
	cflags: func(target, headerPath string) []string {
		libdir := filepath.Join(goenv.Get("TINYGOROOT"), "lib/bdwgc")
		flags := []string{
			// use a modern environment
			"-DUSE_MMAP",              // mmap is available
			"-DUSE_MUNMAP",            // return memory to the OS using munmap
			"-DGC_BUILTIN_ATOMIC",     // use compiler intrinsics for atomic operations
			"-DNO_EXECUTE_PERMISSION", // don't make the heap executable

			// specific flags for TinyGo
			"-DALL_INTERIOR_POINTERS",  // scan interior pointers (needed for Go)
			"-DIGNORE_DYNAMIC_LOADING", // we don't support dynamic loading at the moment
			"-DNO_GETCONTEXT",          // musl doesn't support getcontext()
			// Each goroutine stack is pushed as one root under the threads
			// scheduler, and GC_push_all aborts rather than grow the mark stack.
			"-DINITIAL_MARK_STACK_SIZE=(64*HBLKSIZE)",
			// Return free blocks to the OS after 3 collections rather than 7.
			"-DMUNMAP_THRESHOLD=3",

			// Use a minimal environment.
			"-DNO_MSGBOX_ON_ERROR", // don't call MessageBoxA on Windows
			"-DDONT_USE_ATEXIT",
			"-DNO_GETENV",               // smaller binary, more predictable configuration
			"-DNO_DEBUGGING",            // reduce code size
			"-DGC_ATOMIC_UNCOLLECTABLE", // pointer-free storage retained until GC_free

			// Special flag to work around the lack of __data_start in ld.lld.
			// TODO: try to fix this in LLVM/lld directly so we don't have to
			// work around it anymore.
			"-DGC_DONT_REGISTER_MAIN_STATIC_DATA",

			// Do not scan the stack. We have our own mechanism to do this.
			"-DSTACK_NOT_SCANNED",
			"-DNO_PROC_STAT",  // we scan the stack manually (don't read /proc/self/stat on Linux)
			"-DSTACKBOTTOM=0", // dummy value, we scan the stack manually

			// Assertions can be enabled while debugging GC issues.
			//"-DGC_ASSERTIONS",

			"-I" + libdir + "/include",
		}
		return flags
	},
	configFlags: func(config *compileopts.Config) []string {
		if !config.BoehmThreads() {
			return []string{
				"-DGC_DISABLE_INCREMENTAL", // don't mess with SIGSEGV and such
				"-DNO_CLOCK",               // don't use system clock
			}
		}
		var flags []string
		if arch := config.GOARCH(); arch != "amd64" && arch != "386" {
			// Only x86 Linux has soft dirty bits.
			flags = append(flags, "-DGC_DISABLE_INCREMENTAL")
		}
		// The cores scheduler registers its threads with bdwgc, which stops
		// them for a collection. See src/runtime/gc_boehm_threads.go.
		return append(flags,
			"-DGC_THREADS",
			"-DTHREAD_LOCAL_ALLOC",
			// Stop with SIGRTMIN+6 and restart with SIGRTMIN+5. Real time signals
			// queue, so the 3 ms polling for lost signals is not needed.
			"-DGC_USESIGRT_SIGNALS",
			"-DNO_RETRY_SIGNALS",
			// Mark on every processor while the world is stopped.
			"-DPARALLEL_MARK",
			// Incremental and generational collection finds dirty pages with the
			// soft dirty bits in /proc/self/pagemap. Write protecting the heap
			// would take SIGSEGV, and make system calls fail with EFAULT.
			"-DNO_MPROTECT_VDB",
			// Kinds 4 and 5 are the typed object and typed array kinds, made
			// first by tinygo_runtime_bdwgc_threads_init. Give them local free lists.
			"-DTHREAD_FREELISTS_KINDS=6",
		)
	},
	configSources: func(config *compileopts.Config) []string {
		if !config.BoehmThreads() {
			return nil
		}
		return []string{
			"mallocx.c",
			"pthread_start.c",
			"pthread_stop_world.c",
			"pthread_support.c",
			"thread_local_alloc.c",
		}
	},
	needsLibc: true,
	sourceDir: func() string {
		return filepath.Join(goenv.Get("TINYGOROOT"), "lib/bdwgc")
	},
	librarySources: func(target string, _ bool) ([]string, error) {
		sources := []string{
			"allchblk.c",
			"alloc.c",
			"blacklst.c",
			"dbg_mlc.c",
			"dyn_load.c",
			"finalize.c", // typed arrays use disappearing links
			"headers.c",
			"mach_dep.c",
			"malloc.c",
			"mark.c",
			"mark_rts.c",
			"misc.c",
			"new_hblk.c",
			"os_dep.c",
			"reclaim.c",
			"typd_mlc.c", // typed allocation support
		}
		if strings.Split(target, "-")[2] == "windows" {
			// Due to how the linker on Windows works (that doesn't allow
			// undefined functions), we need to include these extra files.
			sources = append(sources,
				"mallocx.c",
				"ptr_chck.c",
			)
		}
		return sources, nil
	},
}
