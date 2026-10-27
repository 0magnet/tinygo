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
#include <time.h>

#include <gc/gc.h>
#include <gc/gc_typed.h>

void tinygo_runtime_bdwgc_event(int event);
void tinygo_cores_gc_record(void);

static struct sigaction suspend_next;

static void GC_CALLBACK collection_event(GC_EventType event) {
    tinygo_runtime_bdwgc_event((int)event);
}

int tinygo_runtime_bdwgc_threads_init(void) {
    GC_set_on_collection_event(collection_event);

    // Make the typed kinds now, so they get the thread local kind numbers
    // set by THREAD_FREELISTS_KINDS in builder/bdwgc.go.
    GC_word bitmap = 0;
    GC_make_descriptor(&bitmap, 1);

    // GOGC sets how far the heap grows between collections, as
    // debug.SetGCPercent does.
    const char *s = getenv("GOGC");
    long n;
    if (s != NULL && (n = atol(s)) > 0) {
        GC_set_free_space_divisor(n >= 200 ? 1 : 200 / n);
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
