//go:build none

// Ignore the //go:build above. This file is manually included on Linux to
// start child processes for os.StartProcess.

#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <sched.h>
#include <signal.h>
#include <stdint.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <sys/prctl.h>
#include <sys/syscall.h>
#include <unistd.h>

#ifndef SYS_close_range
#define SYS_close_range 436
#endif

// 32-bit arm and 386 have 16-bit ids on the plain calls.
#ifdef SYS_setuid32
#define SYS_SETUID SYS_setuid32
#define SYS_SETGID SYS_setgid32
#define SYS_SETGROUPS SYS_setgroups32
#else
#define SYS_SETUID SYS_setuid
#define SYS_SETGID SYS_setgid
#define SYS_SETGROUPS SYS_setgroups
#endif

// Must match execAttr in exec_linux.go.
struct tinygo_exec_attr {
    const char *path;
    char *const *argv;
    char *const *envp;
    const char *dir;
    int32_t *fds;
    uint32_t *groups;
    int32_t nfds;
    int32_t ngroups;
    int32_t flags;
    int32_t pgid;
    int32_t ctty;
    int32_t pdeathsig;
    uint32_t uid;
    uint32_t gid;
    int32_t err;
};

#define EXEC_SETSID     1
#define EXEC_SETPGID    2
#define EXEC_SETCTTY    4
#define EXEC_NOCTTY     8
#define EXEC_CREDENTIAL 16
#define EXEC_SETGROUPS  32

#define CHILD_STACK_SIZE (64 * 1024)

struct child_arg {
    struct tinygo_exec_attr *a;
    sigset_t oldmask;
};

// Kernel sigaction layout for the raw rt_sigaction call.
struct kernel_sigaction {
    void *handler;
    unsigned long flags;
    void *restorer;
    uint64_t mask;
};

#define CHECK(call) do { if ((call) < 0) goto fail; } while (0)

// Runs in the child on its own stack while the parent thread is suspended.
// Memory is shared with the parent, so it makes only raw system calls.
static int child(void *arg) {
    struct child_arg *c = arg;
    struct tinygo_exec_attr *a = c->a;

    // Drop the runtime's handlers so no Go handler runs here. Ignored
    // signals stay ignored, as in Go.
    for (int sig = 1; sig < 65; sig++) {
        if (sig == SIGKILL || sig == SIGSTOP) {
            continue;
        }
        struct kernel_sigaction old = {0};
        if (syscall(SYS_rt_sigaction, sig, NULL, &old, 8) != 0) {
            continue;
        }
        if (old.handler == (void *)SIG_IGN || old.handler == (void *)SIG_DFL) {
            continue;
        }
        struct kernel_sigaction def = {0};
        syscall(SYS_rt_sigaction, sig, &def, NULL, 8);
    }
    CHECK(syscall(SYS_rt_sigprocmask, SIG_SETMASK, &c->oldmask, NULL, 8));

    if (a->flags & EXEC_SETSID) {
        CHECK(syscall(SYS_setsid));
    }
    if (a->flags & EXEC_SETPGID) {
        CHECK(syscall(SYS_setpgid, 0, a->pgid));
    }
    if (a->flags & EXEC_CREDENTIAL) {
        if (a->flags & EXEC_SETGROUPS) {
            CHECK(syscall(SYS_SETGROUPS, a->ngroups, a->groups));
        }
        CHECK(syscall(SYS_SETGID, a->gid));
        CHECK(syscall(SYS_SETUID, a->uid));
    }
    if (a->dir) {
        CHECK(syscall(SYS_chdir, a->dir));
    }
    if (a->pdeathsig) {
        CHECK(syscall(SYS_prctl, PR_SET_PDEATHSIG, a->pdeathsig, 0, 0, 0));
    }

    // Move every source fd below its target out of the way first, so placing
    // fd i cannot clobber a later source. As in Go's syscall/exec_linux.go.
    int n = a->nfds;
    int32_t *fd = a->fds;
    int nextfd = n;
    for (int i = 0; i < n; i++) {
        if (fd[i] >= 0 && fd[i] < i) {
            if (nextfd == i) {
                nextfd++;
            }
            CHECK(syscall(SYS_dup3, fd[i], nextfd, O_CLOEXEC));
            fd[i] = nextfd;
            nextfd++;
        }
    }
    for (int i = 0; i < n; i++) {
        if (fd[i] == -1) {
            syscall(SYS_close, i);
        } else if (fd[i] == i) {
            CHECK(syscall(SYS_fcntl, i, F_SETFD, 0));
        } else {
            CHECK(syscall(SYS_dup3, fd[i], i, 0));
        }
    }

    if (a->flags & EXEC_NOCTTY) {
        CHECK(syscall(SYS_ioctl, 0, TIOCNOTTY, 0));
    }
    if (a->flags & EXEC_SETCTTY) {
        CHECK(syscall(SYS_ioctl, a->ctty, TIOCSCTTY, 1));
    }

    // Nothing above nfds is meant for the child. Kernels before 5.9 lack
    // close_range, and then only fds opened with O_CLOEXEC are closed.
    syscall(SYS_close_range, n, ~0U, 4 /* CLOSE_RANGE_CLOEXEC */);

    syscall(SYS_execve, a->path, a->argv, a->envp);
fail:
    a->err = errno;
    syscall(SYS_exit, 127);
    return 0;
}

// Start the process described by a and return its pid, or -1 with a->err set.
// The calling thread is suspended until the child calls execve or exits.
int tinygo_exec_start(struct tinygo_exec_attr *a) {
    a->err = 0;
    void *stack = mmap(NULL, CHILD_STACK_SIZE, PROT_READ | PROT_WRITE,
                       MAP_PRIVATE | MAP_ANONYMOUS | MAP_STACK, -1, 0);
    if (stack == MAP_FAILED) {
        a->err = errno;
        return -1;
    }

    struct child_arg c = { .a = a };
    sigset_t all;
    sigfillset(&all);
    pthread_sigmask(SIG_BLOCK, &all, &c.oldmask);
    int pid = clone(child, (char *)stack + CHILD_STACK_SIZE,
                    CLONE_VM | CLONE_VFORK | SIGCHLD, &c, NULL, NULL, NULL);
    int cloneErr = errno;
    pthread_sigmask(SIG_SETMASK, &c.oldmask, NULL);
    munmap(stack, CHILD_STACK_SIZE);

    if (pid < 0) {
        a->err = cloneErr;
        return -1;
    }
    if (a->err != 0) {
        // The child failed before execve and exited, so reap it here.
        int status;
        while (syscall(SYS_wait4, pid, &status, 0, NULL) < 0 && errno == EINTR) {
        }
        return -1;
    }
    return pid;
}
