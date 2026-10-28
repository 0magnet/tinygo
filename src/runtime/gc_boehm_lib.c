//go:build none

// Built as part of bdwgc with its own flags, see builder/bdwgc.go, since it
// reads counters that bdwgc keeps to itself.

#include "private/gc_priv.h"

// tinygo_runtime_bdwgc_marked returns the bytes the last collection marked,
// which GC_start_reclaim counts per block, and sets *trigger to the bytes
// bdwgc allocates before it collects again, as min_bytes_allocd in alloc.c
// computes it. The caller holds the allocator lock.
GC_API GC_word GC_CALL tinygo_runtime_bdwgc_marked(GC_word *trigger) {
    word stack = 0;
#ifdef THREADS
    if (GC_need_to_lock) {
        stack = GC_total_stacksize;
    }
#endif
    word scan = 2 * GC_composite_in_use + GC_atomic_in_use / 4 + 2 * stack + GC_root_size;
    word n = scan / GC_get_free_space_divisor();
    if (GC_incremental) {
        n /= 2;
    }
    if (n < GC_get_min_bytes_allocd()) {
        n = GC_get_min_bytes_allocd();
    }
    *trigger = n;
    return GC_composite_in_use + GC_atomic_in_use;
}
