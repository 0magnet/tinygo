//go:build none

// Threads for the cores scheduler on Linux. Each core is a pthread that runs
// the scheduler loop. Included on Linux (despite the //go:build line above),
// it is unused under the other schedulers.

#define _GNU_SOURCE
#include <pthread.h>
#include <semaphore.h>
#include <signal.h>
#include <stdint.h>

// BDWGC also uses SIGRTMIN+6 on Linux, as does the threads scheduler.
#define coresPauseSignal (SIGRTMIN + 6)

static __thread uint32_t tinygo_cpu;

void tinygo_signal_thread_init(int is_main);
void tinygo_cores_gc_pause(int sig);
void tinygo_cores_run(uint32_t cpu);

struct core_start {
    uint32_t   cpu;
    uintptr_t *top;
    sem_t      started;
};

uint32_t tinygo_cores_current(void) {
    return tinygo_cpu;
}

void tinygo_cores_init(pthread_t *thread) {
    *thread = pthread_self();
    struct sigaction act = { 0 };
    act.sa_handler = tinygo_cores_gc_pause;
    act.sa_flags = SA_RESTART;
    sigfillset(&act.sa_mask);
    sigaction(coresPauseSignal, &act, NULL);
}

static void *core_main(void *arg) {
    struct core_start *s = arg;
    uint32_t cpu = s->cpu;
    tinygo_cpu = cpu;
    tinygo_signal_thread_init(0);
    int top;
    *s->top = (uintptr_t)&top;
    sem_post(&s->started);
    tinygo_cores_run(cpu);
    return NULL;
}

int tinygo_cores_start(uint32_t cpu, uintptr_t stackSize, uintptr_t *top, pthread_t *thread) {
    struct core_start s = { .cpu = cpu, .top = top };
    sem_init(&s.started, 0, 0);
    pthread_attr_t attrs;
    pthread_attr_init(&attrs);
    pthread_attr_setdetachstate(&attrs, PTHREAD_CREATE_DETACHED);
    pthread_attr_setstacksize(&attrs, stackSize);
    int result = pthread_create(thread, &attrs, core_main, &s);
    pthread_attr_destroy(&attrs);
    if (result == 0) {
        sem_wait(&s.started);
    }
    sem_destroy(&s.started);
    return result;
}

void tinygo_cores_signal(pthread_t thread) {
    pthread_kill(thread, coresPauseSignal);
}

void tinygo_cores_spin(void) {
#if defined(__x86_64__) || defined(__i386__)
    __builtin_ia32_pause();
#elif defined(__aarch64__) || defined(__arm__)
    __asm__ __volatile__("yield");
#endif
}
