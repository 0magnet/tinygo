//go:build none

// bdwgc's allchblk.c, built in its place by builder/bdwgc.go. GC_merge_unmapped
// walks the free lists once rather than again after every merge, and only when
// GC_freehblk left a free block next to another since the last walk.

#define GC_merge_unmapped GC_merge_unmapped_bdwgc
#define GC_freehblk GC_freehblk_bdwgc
#include "allchblk.c"
#undef GC_merge_unmapped
#undef GC_freehblk

GC_INNER void GC_freehblk(struct hblk *hbp);

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

#else

GC_INNER void
GC_freehblk(struct hblk *hbp)
{
  GC_freehblk_bdwgc(hbp);
}

#  ifdef USE_MUNMAP
GC_INNER GC_bool GC_merge_unmapped(void);

GC_INNER GC_bool
GC_merge_unmapped(void)
{
  return GC_merge_unmapped_bdwgc();
}
#  endif

#endif
