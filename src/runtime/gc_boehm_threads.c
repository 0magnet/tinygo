//go:build none

// Included in builds where bdwgc has thread support (see BoehmThreads in
// compileopts), despite the //go:build line above.

#define _GNU_SOURCE
#define GC_THREADS
#define GC_NO_THREAD_REDIRECTS
#include <pthread.h>
#include <signal.h>
#include <stdint.h>
#include <stdlib.h>
#include <fcntl.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#include <gc/gc.h>
#include <gc/gc_typed.h>

void tinygo_runtime_bdwgc_event(int event);
void tinygo_cores_gc_record(void);
void tinygo_runtime_bdwgc_pace(void);
extern void (*tinygo_runtime_bdwgc_pace_hook)(void);

static struct sigaction suspend_next;

static GC_warn_proc warn_next;

// An allocation fails at the heap cap of the memory limit, and the runtime
// then collects or lifts the cap, so do not warn about it.
static void GC_CALLBACK warn_proc(const char *msg, GC_uintptr_t arg) {
    if (strstr(msg, "Returning NULL") == NULL) {
        warn_next(msg, arg);
    }
}

static void GC_CALLBACK collection_event(GC_EventType event) {
    tinygo_runtime_bdwgc_event((int)event);
}

// Near the memory limit, the share of time the world may be stopped, in
// percent, before the heap grows past the limit. From TINYGO_GC_LIMIT_PAUSE.
int tinygo_runtime_bdwgc_limit_pause = 75;

// GOGC, which debug.SetGCPercent changes. See gcPace.
int tinygo_runtime_bdwgc_gc_percent = 100;

int tinygo_runtime_bdwgc_threads_init(void) {
    const char *p = getenv("TINYGO_GC_LIMIT_PAUSE");
    if (p != NULL && atoi(p) > 0) {
        tinygo_runtime_bdwgc_limit_pause = atoi(p);
    }
    GC_set_on_collection_event(collection_event);
    tinygo_runtime_bdwgc_pace_hook = tinygo_runtime_bdwgc_pace;
    warn_next = GC_get_warn_proc();
    GC_set_warn_proc(warn_proc);

    // Make the typed kinds now, so they get the thread local kind numbers
    // set by THREAD_FREELISTS_KINDS in builder/bdwgc.go.
    GC_word bitmap = 0;
    GC_make_descriptor(&bitmap, 1);

    // The runtime paces each collection from GOGC once marking finished.
    const char *s = getenv("GOGC");
    long n;
    if (s != NULL && (n = atol(s)) > 0) {
        tinygo_runtime_bdwgc_gc_percent = n > 1 << 20 ? 1 << 20 : (int)n;
    }

    // By default a collection marks in one parallel pause.
    // TINYGO_GC_INCREMENTAL=1 marks in steps of TINYGO_GC_PAUSE_MS with the
    // world stopped and the rest while it runs, then finishes in one pause
    // that marks the dirty pages again on one thread.
    s = getenv("TINYGO_GC_INCREMENTAL");
    if (s != NULL && s[0] == '1') {
        long ms = 10;
        if ((s = getenv("TINYGO_GC_PAUSE_MS")) != NULL) {
            ms = atol(s);
        }
        GC_set_time_limit(ms > 0 ? (unsigned long)ms : GC_TIME_UNLIMITED);
        // Finish each collection in one pause rather than retry in steps.
        GC_set_max_prior_attempts(0);
        GC_enable_incremental();
    }

    // TINYGO_GC_TRACE=1 prints the length of each world stop.
    s = getenv("TINYGO_GC_TRACE");
    return s != NULL && s[0] == '1';
}

// tinygo_runtime_bdwgc_clock reads a clock without allocating, for the
// collection events.
uint64_t tinygo_runtime_bdwgc_clock(int realtime) {
    struct timespec ts;
    clock_gettime(realtime ? CLOCK_REALTIME : CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000 + (uint64_t)ts.tv_nsec;
}

// tinygo_runtime_bdwgc_memory_limit parses GOMEMLIMIT as Go does: bytes with
// an optional B, KiB, MiB, GiB or TiB suffix. It returns -1 when it is unset,
// off or invalid.
int64_t tinygo_runtime_bdwgc_memory_limit(void) {
    static const struct { const char *suffix; int shift; } units[] = {
        { "", 0 }, { "B", 0 }, { "KiB", 10 }, { "MiB", 20 }, { "GiB", 30 }, { "TiB", 40 },
    };
    const char *s = getenv("GOMEMLIMIT");
    char *end;
    if (s == NULL || *s < '0' || *s > '9') {
        return -1;
    }
    uint64_t n = strtoull(s, &end, 10);
    for (size_t i = 0; i < sizeof(units) / sizeof(units[0]); i++) {
        if (strcmp(end, units[i].suffix) == 0) {
            if (n > (uint64_t)INT64_MAX >> units[i].shift) {
                return -1;
            }
            return (int64_t)(n << units[i].shift);
        }
    }
    return -1;
}

// tinygo_runtime_bdwgc_rss reads the resident size from /proc/self/statm
// without allocating.
uintptr_t tinygo_runtime_bdwgc_rss(void) {
    char buf[128];
    int fd = open("/proc/self/statm", O_RDONLY | O_CLOEXEC);
    if (fd < 0) {
        return 0;
    }
    ssize_t n = read(fd, buf, sizeof(buf) - 1);
    close(fd);
    if (n <= 0) {
        return 0;
    }
    buf[n] = 0;
    char *p = strchr(buf, ' ');
    if (p == NULL) {
        return 0;
    }
    return (uintptr_t)strtoull(p + 1, NULL, 10) * (uintptr_t)sysconf(_SC_PAGESIZE);
}

// bdwgc keeps an empty block mapped for this many collections. There is no
// setter, so set it with the allocator lock held.
extern unsigned GC_unmap_threshold;

static void *set_unmap_threshold(void *n) {
    GC_unmap_threshold = *(unsigned *)n;
    return NULL;
}

void tinygo_runtime_bdwgc_set_unmap_threshold(unsigned n) {
    GC_call_with_alloc_lock(set_unmap_threshold, &n);
}

// A core records its stacks, then waits in the bdwgc handler.
static void suspend_handler(int sig, siginfo_t *info, void *context) {
    tinygo_cores_gc_record();
    if (suspend_next.sa_flags & SA_SIGINFO) {
        suspend_next.sa_sigaction(sig, info, context);
    } else if (suspend_next.sa_handler != SIG_DFL && suspend_next.sa_handler != SIG_IGN) {
        suspend_next.sa_handler(sig);
    }
}

void tinygo_runtime_bdwgc_chain_suspend(pthread_t *thread) {
    *thread = pthread_self();
    int sig = GC_get_suspend_signal();
    struct sigaction act;
    sigaction(sig, NULL, &suspend_next);
    act = suspend_next;
    act.sa_flags |= SA_SIGINFO;
    act.sa_sigaction = suspend_handler;
    sigaction(sig, &act, NULL);
}
