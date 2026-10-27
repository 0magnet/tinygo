//go:build none

// This file is included in the build on systems that support the Boehm GC,
// despite the //go:build line above.

#include <stdint.h>
#include <string.h>

#include <gc/gc_mark.h>
#include <gc/gc_typed.h>

void tinygo_runtime_bdwgc_callback(void);

// The descriptor cache is read without a lock, since bdwgc may allocate on
// several threads at once. Entries never change once published, and a grown
// table gets copies of them, so a reader never follows a relinked chain.
struct descriptor_cache_entry {
    uintptr_t layout;
    GC_descr descriptor;
    struct descriptor_cache_entry *next;
};

struct descriptor_cache_table {
    size_t capacity;
    size_t count;
    struct descriptor_cache_entry *buckets[];
};

static struct descriptor_cache_table *descriptor_cache;
static char descriptor_cache_lock;

static size_t descriptor_cache_index(uintptr_t layout, size_t capacity) {
#if UINTPTR_MAX > UINT32_MAX
    layout ^= layout >> 33;
#endif
    layout ^= layout >> 16;
    layout *= 0x45d9f3b;
    layout ^= layout >> 16;
    return layout & (capacity - 1);
}

static GC_descr descriptor_cache_find(struct descriptor_cache_table *table,
                                      uintptr_t layout) {
    struct descriptor_cache_entry *entry;

    if (table == NULL) {
        return 0;
    }
    entry = __atomic_load_n(
        &table->buckets[descriptor_cache_index(layout, table->capacity)],
        __ATOMIC_ACQUIRE);
    for (; entry != NULL; entry = entry->next) {
        if (entry->layout == layout) {
            return entry->descriptor;
        }
    }
    return 0;
}

static int descriptor_cache_add(struct descriptor_cache_table *table,
                                uintptr_t layout, GC_descr descriptor) {
    struct descriptor_cache_entry *entry;
    size_t index = descriptor_cache_index(layout, table->capacity);

    entry = GC_malloc_atomic_uncollectable(sizeof(*entry));
    if (entry == NULL) {
        return 0;
    }
    entry->layout = layout;
    entry->descriptor = descriptor;
    entry->next = table->buckets[index];
    __atomic_store_n(&table->buckets[index], entry, __ATOMIC_RELEASE);
    table->count++;
    return 1;
}

// grow_descriptor_cache publishes a table twice as large. The old table and
// its entries stay, as readers may still use them.
static int grow_descriptor_cache(void) {
    struct descriptor_cache_table *old = descriptor_cache, *table;
    size_t capacity = old == NULL ? 64 : old->capacity * 2;
    size_t size = sizeof(*table) + capacity * sizeof(table->buckets[0]);
    size_t i;

    table = GC_malloc_atomic_uncollectable(size);
    if (table == NULL) {
        return 0;
    }
    memset(table, 0, size);
    table->capacity = capacity;
    for (i = 0; old != NULL && i < old->capacity; i++) {
        struct descriptor_cache_entry *entry;
        for (entry = old->buckets[i]; entry != NULL; entry = entry->next) {
            if (!descriptor_cache_add(table, entry->layout, entry->descriptor)) {
                GC_free(table);
                return 0;
            }
        }
    }
    __atomic_store_n(&descriptor_cache, table, __ATOMIC_RELEASE);
    return 1;
}

static void GC_CALLBACK callback(void) {
    tinygo_runtime_bdwgc_callback();
}

static void GC_CALLBACK warn_proc(const char *msg, GC_word arg) {
}

void tinygo_runtime_bdwgc_init(void) {
    GC_set_push_other_roots(callback);
#if defined(__wasm__)
    // There are a lot of warnings on WebAssembly in the form:
    //
    //     GC Warning: Repeated allocation of very large block (appr. size 68 KiB):
    //         May lead to memory leak and poor performance
    //
    // The usual advice is to use something like GC_malloc_ignore_off_page but
    // unfortunately for most allocations that's not allowed: Go allocations can
    // legitimately hold pointers further than one page in the allocation. So
    // instead we just disable the warning.
    GC_set_warn_proc(warn_proc);
#endif
}

static GC_descr make_descriptor(uintptr_t layout) {
    GC_word inline_bitmap;
    GC_word *bitmap;
    size_t bit_count;
    size_t word_count;
    GC_descr descriptor;

    if (layout & 1) {
        const size_t size_bits = 4 + sizeof(uintptr_t) / 4;
        const uintptr_t size_mask = ((uintptr_t)1 << size_bits) - 1;

        bit_count = (layout >> 1) & size_mask;
        inline_bitmap = layout >> (size_bits + 1);
        bitmap = &inline_bitmap;
    } else {
        const uint8_t *bytes = (const uint8_t *)(layout + sizeof(uintptr_t));
        size_t i;

        bit_count = *(const uintptr_t *)layout;
        word_count = (bit_count + GC_WORDSZ - 1) / GC_WORDSZ;
        bitmap = GC_malloc_atomic_uncollectable(word_count * sizeof(GC_word));
        if (bitmap == NULL) {
            return 0;
        }
        memset(bitmap, 0, word_count * sizeof(GC_word));
        for (i = 0; i < bit_count; i++) {
            if ((bytes[i / 8] >> (i % 8)) & 1) {
                GC_set_bit(bitmap, i);
            }
        }
    }

    descriptor = GC_make_descriptor(bitmap, bit_count);
    if (!(layout & 1)) {
        GC_free(bitmap);
    }
    return descriptor;
}

GC_descr tinygo_runtime_bdwgc_make_descriptor(uintptr_t layout) {
    struct descriptor_cache_table *table;
    GC_descr descriptor;

    descriptor = descriptor_cache_find(
        __atomic_load_n(&descriptor_cache, __ATOMIC_ACQUIRE), layout);
    if (descriptor != 0) {
        return descriptor;
    }

    // Misses are rare, one per layout, so writers just spin.
    while (__atomic_test_and_set(&descriptor_cache_lock, __ATOMIC_ACQUIRE)) {
    }
    table = descriptor_cache;
    descriptor = descriptor_cache_find(table, layout);
    if (descriptor == 0 &&
        ((table != NULL && table->count < table->capacity * 2) ||
         grow_descriptor_cache())) {
        descriptor = make_descriptor(layout);
        if (descriptor != 0 && !descriptor_cache_add(descriptor_cache, layout, descriptor)) {
            descriptor = 0;
        }
    }
    __atomic_clear(&descriptor_cache_lock, __ATOMIC_RELEASE);
    return descriptor;
}
