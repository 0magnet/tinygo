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

extern GC_word tinygo_runtime_bdwgc_pace_bytes;

// tinygo_runtime_bdwgc_set_pace makes bdwgc collect once trigger bytes were
// allocated. GC_should_collect in alloc.c compares them with
// min_bytes_allocd, the larger of scan/GC_free_space_divisor and
// GC_set_min_bytes_allocd, so the divisor is raised until the first is
// smaller. The caller holds the allocator lock.
GC_API void GC_CALL tinygo_runtime_bdwgc_set_pace(GC_word trigger) {
    word stack = 0;
#ifdef THREADS
    if (GC_need_to_lock) {
        stack = GC_total_stacksize;
    }
#endif
    word scan = 2 * GC_composite_in_use + GC_atomic_in_use / 4 + 2 * stack + GC_root_size;
    if (trigger < HBLKSIZE) {
        trigger = HBLKSIZE;
    }
    GC_set_free_space_divisor(scan / trigger + 1);
    GC_set_min_bytes_allocd(trigger);
    tinygo_runtime_bdwgc_pace_bytes = trigger;
}
