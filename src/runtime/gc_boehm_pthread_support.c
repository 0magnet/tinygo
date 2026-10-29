//go:build none

// bdwgc's pthread_support.c, built in its place by builder/bdwgc.go. On Linux
// it counts processors in /proc/stat, which ProcSubset=pid in a systemd unit
// hides, and then marks on one thread. Count them with sysconf instead then.

#include "private/gc_priv.h"

static const char *tinygo_bdwgc_getenv(const char *name);

#undef GETENV
#define GETENV(name) tinygo_bdwgc_getenv(name)
#include "pthread_support.c"

// GETENV is NULL with NO_GETENV. Give GC_thr_init GC_NPROCS from the
// environment, or the online processors when /proc/stat cannot be read.
static const char *tinygo_bdwgc_getenv(const char *name) {
    static char buf[16];
    if (strcmp(name, "GC_NPROCS") != 0) {
        return NULL;
    }
    const char *s = getenv(name);
    if (s != NULL) {
        return s;
    }
    int f = open("/proc/stat", O_RDONLY);
    if (f >= 0) {
        close(f);
        return NULL;
    }
    long n = sysconf(_SC_NPROCESSORS_ONLN);
    if (n <= 0) {
        return NULL;
    }
    snprintf(buf, sizeof(buf), "%ld", n);
    return buf;
}
