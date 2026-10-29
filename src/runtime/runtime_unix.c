//go:build none

// This file is included on Darwin and Linux (despite the //go:build line above).

#define _GNU_SOURCE
#define _XOPEN_SOURCE
#include <signal.h>
#include <unistd.h>
#include <stdint.h>
#include <ucontext.h>
#include <string.h>
#if __linux__
#include <pthread.h>
#include <sys/resource.h>
#include <stdlib.h>
#endif

void tinygo_handle_fatal_signal(int sig, int code, uintptr_t fault, uintptr_t pc, uintptr_t fp);

// tinygo_sigpanic is defined in Go. It turns a signal into a Go panic
// that can be recovered with recover(). The signal number is passed via
// the tinygo_caught_signal global.
void tinygo_sigpanic(void);

// Set by the signal handler before redirecting to tinygo_sigpanic.
int tinygo_caught_signal;

// The signal, si_code, si_addr and faulting pc of the last signal this thread
// redirected to tinygo_sigpanic, which takes them before anything can unwind.
static __thread uintptr_t caught_info[4];

uintptr_t tinygo_take_signal_info(int i) {
	uintptr_t v = caught_info[i];
	caught_info[i] = 0;
	return v;
}

static uintptr_t context_pc(ucontext_t *uctx) {
#if __APPLE__
	#if __arm64__
		return uctx->uc_mcontext->__ss.__pc;
	#elif __x86_64__
		return uctx->uc_mcontext->__ss.__rip;
	#else
		#error unknown architecture
	#endif
#elif __linux__
	// Note: this can probably be simplified using the MC_PC macro in musl,
	// but this works for now.
	#if __arm__
		return uctx->uc_mcontext.arm_pc;
	#elif __i386__
		return uctx->uc_mcontext.gregs[REG_EIP];
	#elif __x86_64__
		return uctx->uc_mcontext.gregs[REG_RIP];
	#else // aarch64, mips, maybe others
		return uctx->uc_mcontext.pc;
	#endif
#else
	#error unknown platform
#endif
}

// context_sp returns the stack pointer where the signal hit on Linux, and
// zero where it is not known.
__attribute__((unused)) static uintptr_t context_sp(ucontext_t *uctx) {
#if __linux__ && __x86_64__
	return uctx->uc_mcontext.gregs[REG_RSP];
#elif __linux__ && __aarch64__
	return uctx->uc_mcontext.sp;
#else
	return 0;
#endif
}

// context_fp returns the frame pointer where the signal hit, where TinyGo
// keeps frame pointers, and zero elsewhere.
static uintptr_t context_fp(ucontext_t *uctx) {
#if __linux__ && __x86_64__
	return uctx->uc_mcontext.gregs[REG_RBP];
#elif __linux__ && __aarch64__
	return uctx->uc_mcontext.regs[29];
#else
	return 0;
#endif
}

// Whether sigpanic-based recovery is supported for the current
// architecture. Set to 0 on architectures where we can't reliably
// modify the ucontext to redirect execution.
static int can_sigpanic(void) {
#if defined(__x86_64__) || defined(__i386__) || defined(__aarch64__) || defined(__arm64__) || defined(__arm__) || defined(__mips__)
	return 1;
#else
	return 0;
#endif
}

// Try to redirect execution from the signal handler to tinygo_sigpanic.
// Returns 1 on success, 0 if the architecture doesn't support it.
static int redirect_to_sigpanic(int sig, siginfo_t *info, ucontext_t *uctx) {
	if (!can_sigpanic()) {
		return 0;
	}
	tinygo_caught_signal = sig;
	caught_info[0] = sig;
	caught_info[1] = (uintptr_t)info->si_code;
	caught_info[2] = (uintptr_t)info->si_addr;
	caught_info[3] = context_pc(uctx);

#if __APPLE__
	#if __arm64__
		// ARM64: set LR to the faulting PC (as return address), set PC to sigpanic
		uctx->uc_mcontext->__ss.__lr = uctx->uc_mcontext->__ss.__pc;
		uctx->uc_mcontext->__ss.__pc = (uint64_t)&tinygo_sigpanic;
	#elif __x86_64__
		// x86_64: push the faulting PC onto the stack, set RIP to sigpanic
		uintptr_t sp = uctx->uc_mcontext->__ss.__rsp;
		sp -= sizeof(uintptr_t);
		*(uintptr_t *)sp = uctx->uc_mcontext->__ss.__rip;
		uctx->uc_mcontext->__ss.__rsp = sp;
		uctx->uc_mcontext->__ss.__rip = (uint64_t)&tinygo_sigpanic;
	#else
		return 0;
	#endif
#elif __linux__
	#if __x86_64__
		uintptr_t sp = uctx->uc_mcontext.gregs[REG_RSP];
		sp -= sizeof(uintptr_t);
		*(uintptr_t *)sp = uctx->uc_mcontext.gregs[REG_RIP];
		uctx->uc_mcontext.gregs[REG_RSP] = sp;
		uctx->uc_mcontext.gregs[REG_RIP] = (uintptr_t)&tinygo_sigpanic;
	#elif __i386__
		uintptr_t sp = uctx->uc_mcontext.gregs[REG_ESP];
		sp -= sizeof(uintptr_t);
		*(uintptr_t *)sp = uctx->uc_mcontext.gregs[REG_EIP];
		uctx->uc_mcontext.gregs[REG_ESP] = sp;
		uctx->uc_mcontext.gregs[REG_EIP] = (uintptr_t)&tinygo_sigpanic;
	#elif __aarch64__
		uctx->uc_mcontext.regs[30] = uctx->uc_mcontext.pc; // LR = faulting PC
		uctx->uc_mcontext.pc = (uintptr_t)&tinygo_sigpanic;
	#elif __arm__
		uctx->uc_mcontext.arm_lr = uctx->uc_mcontext.arm_pc;
		uctx->uc_mcontext.arm_pc = (uintptr_t)&tinygo_sigpanic;
	#elif defined(__mips__)
		// MIPS: set RA (gregs[31]) to the faulting PC, set PC to sigpanic.
		uctx->uc_mcontext.gregs[31] = uctx->uc_mcontext.pc;
		uctx->uc_mcontext.pc = (uintptr_t)&tinygo_sigpanic;
	#else
		return 0;
	#endif
#else
	return 0;
#endif

	return 1;
}

#if __linux__
// A thread that runs out of stack faults in the guard page below it. The
// handler then needs a stack of its own, and must not push onto the full one.
#define SIGNAL_STACK_SIZE (32 * 1024)
#define OVERFLOW_WINDOW   (64 * 1024)
#define SA_FATAL          (SA_SIGINFO | SA_RESETHAND | SA_ONSTACK)

void tinygo_handle_stack_overflow(int sig, int code, uintptr_t fault, uintptr_t pc, uintptr_t fp);

static __thread uintptr_t stack_lo, stack_hi;

// The bounds of the calling thread's own stack, for a crash trace.
uintptr_t tinygo_thread_stack_lo(void) { return stack_lo; }
uintptr_t tinygo_thread_stack_hi(void) { return stack_hi; }
static pthread_key_t signal_stack_key;
static pthread_once_t signal_stack_once = PTHREAD_ONCE_INIT;

static void signal_stack_free(void *stack) {
	stack_t ss = { .ss_flags = SS_DISABLE };
	sigaltstack(&ss, NULL);
	free(stack);
}

static void signal_stack_key_init(void) {
	pthread_key_create(&signal_stack_key, signal_stack_free);
}

// Give the calling thread a signal stack and record where its stack ends. The
// main thread's stack grows on demand, up to RLIMIT_STACK below its top.
void tinygo_signal_thread_init(int is_main) {
	pthread_attr_t attr;
	if (pthread_getattr_np(pthread_self(), &attr) == 0) {
		void *addr;
		size_t size;
		if (pthread_attr_getstack(&attr, &addr, &size) == 0) {
			stack_lo = (uintptr_t)addr;
			stack_hi = (uintptr_t)addr + size;
			struct rlimit rl;
			if (is_main && getrlimit(RLIMIT_STACK, &rl) == 0 && rl.rlim_cur != RLIM_INFINITY &&
				rl.rlim_cur < stack_lo + size) {
				stack_lo = stack_lo + size - rl.rlim_cur;
			}
		}
		pthread_attr_destroy(&attr);
	}
	void *stack = malloc(SIGNAL_STACK_SIZE);
	if (stack == NULL) {
		return;
	}
	stack_t ss = { .ss_sp = stack, .ss_size = SIGNAL_STACK_SIZE };
	if (sigaltstack(&ss, NULL) != 0) {
		free(stack);
		return;
	}
	pthread_once(&signal_stack_once, signal_stack_key_init);
	pthread_setspecific(signal_stack_key, stack);
}

// A goroutine stack is mapped by the scheduler, so its guard page is found
// from the stack pointer: a fault just below it is an overflow.
static int is_stack_overflow(int sig, siginfo_t *info, ucontext_t *uctx) {
	uintptr_t addr = (uintptr_t)info->si_addr;
	if (sig != SIGSEGV && sig != SIGBUS) {
		return 0;
	}
	uintptr_t sp = context_sp(uctx);
	if (sp != 0 && addr < sp + 256 && addr + OVERFLOW_WINDOW >= sp) {
		return 1;
	}
	return stack_lo != 0 && addr + OVERFLOW_WINDOW >= stack_lo && addr < stack_lo + OVERFLOW_WINDOW;
}
#else
#define SA_FATAL (SA_SIGINFO | SA_RESETHAND)
#endif

static void signal_handler(int sig, siginfo_t *info, void *context) {
	ucontext_t* uctx = context;

#if __linux__
	if (is_stack_overflow(sig, info, uctx)) {
		tinygo_handle_stack_overflow(sig, info->si_code, (uintptr_t)info->si_addr, context_pc(uctx), context_fp(uctx));
		return;
	}
#endif

	// Try to redirect to sigpanic for a recoverable panic.
	if (redirect_to_sigpanic(sig, info, uctx)) {
		// Re-register the signal handler since SA_RESETHAND cleared it.
		// We need it active in case the sigpanic itself faults (e.g.,
		// stack overflow during panic).
		struct sigaction act;
		memset(&act, 0, sizeof(act));
		act.sa_flags = SA_FATAL;
		act.sa_sigaction = &signal_handler;
		sigaction(sig, &act, NULL);
		return; // return from signal handler; execution resumes at sigpanic
	}

	tinygo_handle_fatal_signal(sig, info->si_code, (uintptr_t)info->si_addr, context_pc(uctx), context_fp(uctx));
}

void tinygo_register_fatal_signals(void) {
	// As in Go, a write to a closed pipe or socket returns EPIPE rather than
	// killing the process. os/signal can still ask for SIGPIPE.
	struct sigaction ign = { 0 };
	ign.sa_handler = SIG_IGN;
	sigaction(SIGPIPE, &ign, NULL);

#if __linux__
	tinygo_signal_thread_init(1);
#endif
	struct sigaction act = { 0 };
	// SA_SIGINFO:   we want the 2 extra parameters
	// SA_RESETHAND: only catch the signal once (the handler will re-raise the signal)
	// SA_ONSTACK:   run on the thread's signal stack, on Linux
	act.sa_flags = SA_FATAL;
	act.sa_sigaction = &signal_handler;

	// Register the signal handler for common issues. There are more signals,
	// which can be added if needed.
	sigaction(SIGBUS, &act, NULL);
	sigaction(SIGFPE, &act, NULL);
	sigaction(SIGILL, &act, NULL);
	sigaction(SIGSEGV, &act, NULL);
}
