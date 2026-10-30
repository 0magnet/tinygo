//go:build none

// bdwgc's allchblk.c, built in its place by builder/bdwgc.go. GC_merge_unmapped
// walks the free lists once rather than again after every merge, and only when
// GC_freehblk left a free block next to another since the last walk.
// GC_unmap_old and GC_allochblk keep the resident heap near what the next
// cycle needs, see below.

#include <time.h>
#ifdef _WIN32
#  include <windows.h>
#endif

#define GC_merge_unmapped GC_merge_unmapped_bdwgc
#define GC_freehblk GC_freehblk_bdwgc
#define GC_unmap_old GC_unmap_old_bdwgc
#define GC_allochblk GC_allochblk_bdwgc
#include "allchblk.c"
#undef GC_merge_unmapped
#undef GC_freehblk
#undef GC_unmap_old
#undef GC_allochblk

GC_INNER void GC_freehblk(struct hblk *hbp);
GC_INNER struct hblk *GC_allochblk(size_t lb_adjusted, int kind,
                                  unsigned flags, size_t align_m1);
#ifdef USE_MUNMAP
GC_INNER void GC_unmap_old(unsigned threshold);
#endif

// What may be allocated between collections, set after each one by the runtime
// with thread support, or zero. See tinygo_runtime_bdwgc_set_pace.
GC_word tinygo_runtime_bdwgc_pace_bytes = 0;

// Called after each collection has marked and set up the sweep, before free
// blocks are given back. The allocator lock is held.
void (*tinygo_runtime_bdwgc_pace_hook)(void) = NULL;

#if defined(USE_MUNMAP) && !defined(CHERI_PURECAP)

GC_INNER GC_bool GC_merge_unmapped(void);

// Whether a free block may be next to another free block. Protected by the
// allocator lock.
static GC_bool merge_pending = TRUE;

GC_INNER void
GC_freehblk(struct hblk *hbp)
{
  struct hblk *prev = GC_free_block_ending_at(hbp);
  struct hblk *h, *next;
  hdr *hhdr, *nexthdr;

  GC_freehblk_bdwgc(hbp);
  if (merge_pending)
    return;
  // The freed block was merged into the free block before it, if mapped.
  h = hbp;
  if (prev != NULL && HDR(hbp) == NULL)
    h = prev;
  hhdr = HDR(h);
  next = (struct hblk *)((ptr_t)h + hhdr->hb_sz);
  GET_HDR(next, nexthdr);
  if ((nexthdr != NULL && HBLK_IS_FREE(nexthdr))
      || GC_free_block_ending_at(h) != NULL)
    merge_pending = TRUE;
}

#ifdef GC_ASSERTIONS
// Check that no free block is next to another that it could be merged with.
static void
assert_merged(void)
{
  size_t i;

  for (i = 0; i <= N_HBLK_FLS; ++i) {
    struct hblk *h;
    hdr *hhdr, *nexthdr;

    for (h = GC_hblkfreelist[i]; h != NULL; h = hhdr->hb_next) {
      hhdr = HDR(h);
      GET_HDR((struct hblk *)((ptr_t)h + hhdr->hb_sz), nexthdr);
      GC_ASSERT(NULL == nexthdr || !HBLK_IS_FREE(nexthdr)
                || BLOCKS_MERGE_OVERFLOW(hhdr, nexthdr));
    }
  }
}
#else
#  define assert_merged() (void)0
#endif

GC_INNER GC_bool
GC_merge_unmapped(void)
{
  size_t i;
  GC_bool merged = FALSE;

  if (!merge_pending) {
    assert_merged();
    return FALSE;
  }
  for (i = 0; i <= N_HBLK_FLS; ++i) {
    struct hblk *h = GC_hblkfreelist[i];

    while (h != NULL) {
      hdr *hhdr = HDR(h);
      // Where to go on in list i. A merged block goes to the list for its
      // new size, at or after list i, so it is not lost.
      struct hblk *cont = hhdr->hb_next;
      GC_bool grew = FALSE;

      for (;;) {
        struct hblk *next = (struct hblk *)((ptr_t)h + hhdr->hb_sz);
        hdr *nexthdr;
        size_t size = hhdr->hb_sz, next_size;

        GET_HDR(next, nexthdr);
        if (NULL == nexthdr || !HBLK_IS_FREE(nexthdr)
            || BLOCKS_MERGE_OVERFLOW(hhdr, nexthdr))
          break;
        next_size = nexthdr->hb_sz;
        if (!grew) {
          GC_remove_from_fl_at(hhdr, i);
          grew = TRUE;
        }
        if (next == cont)
          cont = nexthdr->hb_next;

        // As upstream: make both mapped or both unmapped, keeping the
        // state of the larger one.
        if (IS_MAPPED(hhdr) && !IS_MAPPED(nexthdr)) {
          if (size > next_size) {
            GC_adjust_num_unmapped(next, nexthdr);
            GC_remap((ptr_t)next, next_size);
          } else {
            GC_adjust_num_unmapped(h, hhdr);
            GC_unmap((ptr_t)h, size);
            GC_unmap_gap((ptr_t)h, size, (ptr_t)next, next_size);
            hhdr->hb_flags |= WAS_UNMAPPED;
          }
        } else if (IS_MAPPED(nexthdr) && !IS_MAPPED(hhdr)) {
          if (size > next_size) {
            GC_adjust_num_unmapped(next, nexthdr);
            GC_unmap((ptr_t)next, next_size);
            GC_unmap_gap((ptr_t)h, size, (ptr_t)next, next_size);
          } else {
            GC_adjust_num_unmapped(h, hhdr);
            GC_remap((ptr_t)h, size);
            hhdr->hb_flags &= (unsigned char)~WAS_UNMAPPED;
            hhdr->hb_last_reclaimed = nexthdr->hb_last_reclaimed;
          }
        } else if (!IS_MAPPED(hhdr) && !IS_MAPPED(nexthdr)) {
          GC_unmap_gap((ptr_t)h, size, (ptr_t)next, next_size);
        }
        GC_remove_from_fl(nexthdr);
        hhdr->hb_sz += next_size;
        GC_remove_header(next);
      }
      if (grew) {
        GC_add_to_fl(h, hhdr);
        merged = TRUE;
      }
      h = cont;
    }
  }
  assert_merged();
  merge_pending = FALSE;
  return merged;
}

// The NULL results since the last block was allocated, and the collection
// count and heap size at the last one. Protected by the allocator lock.
static unsigned alloc_failures = 0;
static word failed_gc_no, failed_heapsize;

// Without incremental mode bdwgc collects only when an allocation finds no
// free block (GC_allocobj and GC_alloc_large call GC_collect_or_expand only
// then), so the heap fills to whatever size it grew to. Fail once the paced
// bytes were allocated, or once a collection is due and the mapped free
// blocks are used up, so that GC_collect_or_expand collects instead.
//
// GC_alloc_large in malloc.c aborts with "Too many retries" after ten more
// failures, each after GC_collect_or_expand collected or grew the heap. So
// once either happened since a failure, use any free block that fits.
// GC_should_collect stays true for the rest of a cycle in which the heap
// grew, which gcGrowHeap does after most collections, and GC_allochblk_bdwgc
// then does not split large free blocks.
GC_INNER struct hblk *
GC_allochblk(size_t lb_adjusted, int kind, unsigned flags, size_t align_m1)
{
  word unmapped = GC_unmapped_bytes;
  size_t size = OBJ_SZ_TO_BLOCKS(lb_adjusted) * HBLKSIZE;
  GC_bool retry = alloc_failures > 0
                  && (GC_gc_no != failed_gc_no || GC_heapsize != failed_heapsize);
  struct hblk *h;

  if (!retry && !GC_dont_gc && !GC_incremental
      && ((tinygo_runtime_bdwgc_pace_bytes != 0
           && GC_bytes_allocd >= tinygo_runtime_bdwgc_pace_bytes)
          || GC_large_free_bytes - GC_unmapped_bytes < size + align_m1)
      && GC_should_collect()) {
    h = NULL;
  } else if (!retry) {
    h = GC_allochblk_bdwgc(lb_adjusted, kind, flags, align_m1);
  } else {
    int entire = GC_use_entire_heap;

    GC_use_entire_heap = TRUE;
    h = GC_allochblk_bdwgc(lb_adjusted, kind, flags, align_m1);
    // Stale pointers on goroutine stacks black-list pages of free blocks,
    // and bdwgc takes a black-listed block only for a request bigger than
    // GC_black_list_spacing. Accept one with a clean first page, as it does
    // for IGNORE_OFF_PAGE, then set the header up without that flag, which
    // would make interior pointers past the first page not count.
    if (NULL == h && alloc_failures >= 3 && (flags & IGNORE_OFF_PAGE) == 0) {
      h = GC_allochblk_bdwgc(lb_adjusted, kind, flags | IGNORE_OFF_PAGE,
                             align_m1);
      if (h != NULL)
        (void)setup_header(HDR(h), h, lb_adjusted, kind, flags);
    }
    GC_use_entire_heap = entire;
  }
  if (NULL == h) {
    alloc_failures++;
    failed_gc_no = GC_gc_no;
    failed_heapsize = GC_heapsize;
    return NULL;
  }
  alloc_failures = 0;
  // bdwgc maps the whole of an unmapped block to split it. Give the rest,
  // which nothing touched, back again.
  if (h != NULL && GC_unmapped_bytes < unmapped
      && unmapped - GC_unmapped_bytes > size) {
    struct hblk *rest = (struct hblk *)((ptr_t)h + size);
    hdr *rhdr;

    GET_HDR(rest, rhdr);
    if (rhdr != NULL && HBLK_IS_FREE(rhdr) && IS_MAPPED(rhdr)
        && rhdr->hb_sz <= unmapped - GC_unmapped_bytes - size) {
      GC_adjust_num_unmapped(rest, rhdr);
      GC_unmap((ptr_t)rest, rhdr->hb_sz);
      rhdr->hb_flags |= WAS_UNMAPPED;
    }
  }
  return h;
}

// Time spent giving free blocks back per call of GC_unmap_old, in ns.
#  define RELEASE_BUDGET_NS 10000000

static long long
release_clock(void)
{
#  ifdef _WIN32
  // mingw-w64 has clock_gettime only in winpthreads, which TinyGo omits.
  LARGE_INTEGER c, f;

  QueryPerformanceCounter(&c);
  QueryPerformanceFrequency(&f);
  return c.QuadPart / f.QuadPart * 1000000000
         + c.QuadPart % f.QuadPart * 1000000000 / f.QuadPart;
#  else
  struct timespec ts;

  clock_gettime(CLOCK_MONOTONIC, &ts);
  return (long long)ts.tv_sec * 1000000000 + ts.tv_nsec;
#  endif
}

// Put the mapped blocks of free list i before the unmapped ones, so that
// GC_allochblk_nth, which takes the first block that fits, reuses resident
// memory before it faults released pages in again.
static void
mapped_first(size_t i)
{
  struct hblk *h = GC_hblkfreelist[i];
  struct hblk *head[2] = { NULL, NULL }, *tail[2] = { NULL, NULL };

  while (h != NULL) {
    hdr *hhdr = HDR(h);
    struct hblk *next = hhdr->hb_next;
    int u = !IS_MAPPED(hhdr);

    hhdr->hb_prev = tail[u];
    hhdr->hb_next = NULL;
    if (tail[u] != NULL)
      HDR(tail[u])->hb_next = h;
    else
      head[u] = h;
    tail[u] = h;
    h = next;
  }
  if (tail[0] != NULL) {
    HDR(tail[0])->hb_next = head[1];
    if (head[1] != NULL)
      HDR(head[1])->hb_prev = tail[0];
    GC_hblkfreelist[i] = head[0];
  } else {
    GC_hblkfreelist[i] = head[1];
  }
}

// What stays mapped for the next cycle. When paced, what the heap goal and a
// tenth more leave free after the blocks in use, as Go's scavenger keeps.
// Otherwise bdwgc's trigger, as min_bytes_allocd in alloc.c computes it, or a
// quarter of the marked bytes if more.
static word
release_keep(void)
{
  word live = GC_composite_in_use + GC_atomic_in_use;
  word stack = 0, keep;

  if (tinygo_runtime_bdwgc_pace_bytes != 0) {
    word goal = live + tinygo_runtime_bdwgc_pace_bytes;
    word used = GC_heapsize - GC_large_free_bytes;

    goal += goal / 10;
    return goal > used ? goal - used : 0;
  }

#  ifdef THREADS
  if (GC_need_to_lock)
    stack = GC_total_stacksize;
#  endif
  keep = (2 * GC_composite_in_use + GC_atomic_in_use / 4 + 2 * stack
          + GC_root_size)
         / GC_free_space_divisor;
  if (keep < GC_get_min_bytes_allocd())
    keep = GC_get_min_bytes_allocd();
  if (live / 4 > keep)
    keep = live / 4;
  return keep;
}

// Give mapped free blocks back to the OS, largest first, until no more than
// what the next cycle may allocate stays mapped, or the time budget is spent.
static void
release_free(void)
{
  word keep = release_keep();
  word mapped = GC_large_free_bytes - GC_unmapped_bytes;
  long long start = release_clock();
  size_t i = N_HBLK_FLS + 1;

  while (i-- > 0) {
    struct hblk *h;
    hdr *hhdr;

    for (h = GC_hblkfreelist[i]; h != NULL && mapped > keep;
         h = hhdr->hb_next) {
      hhdr = HDR(h);
      if (!IS_MAPPED(hhdr))
        continue;
      if (release_clock() - start > RELEASE_BUDGET_NS) {
        keep = mapped;
        break;
      }
      GC_adjust_num_unmapped(h, hhdr);
      GC_unmap((ptr_t)h, hhdr->hb_sz);
      hhdr->hb_flags |= WAS_UNMAPPED;
      mapped -= hhdr->hb_sz;
    }
    mapped_first(i);
  }
}

// bdwgc unmaps a free block once it has been free for GC_unmap_threshold
// collections, but GC_freehblk makes a block young again whenever a freed
// neighbour joins it, so in a heap with churn nearly every free block is
// young and nothing goes back. Before the heap grows it unmaps every free
// block instead, which then faults back in. Here both calls give back what
// the next cycle will not need, in a bounded time.
GC_INNER void
GC_unmap_old(unsigned threshold)
{
  if (threshold > 0) {
    // GC_finish_collection in alloc.c calls it with a threshold once
    // GC_start_reclaim counted the marked bytes, so the runtime paces here.
    if (tinygo_runtime_bdwgc_pace_hook != NULL)
      tinygo_runtime_bdwgc_pace_hook();
    GC_unmap_old_bdwgc(threshold);
  }
  release_free();
}

#else

GC_INNER void
GC_freehblk(struct hblk *hbp)
{
  GC_freehblk_bdwgc(hbp);
}

GC_INNER struct hblk *
GC_allochblk(size_t lb_adjusted, int kind, unsigned flags, size_t align_m1)
{
  return GC_allochblk_bdwgc(lb_adjusted, kind, flags, align_m1);
}

#  ifdef USE_MUNMAP
GC_INNER GC_bool GC_merge_unmapped(void);

GC_INNER GC_bool
GC_merge_unmapped(void)
{
  return GC_merge_unmapped_bdwgc();
}

GC_INNER void
GC_unmap_old(unsigned threshold)
{
  GC_unmap_old_bdwgc(threshold);
}
#  endif

#endif
