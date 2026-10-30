//go:build none

// Included on linux/386 (see compileopts/target.go). The body behind
// golang.org/x/net/internal/socket's socketcall, see sys_socketcall_386.go.

#include <errno.h>
#include <sys/syscall.h>
#include <unistd.h>

long tinygo_socketcall(long call, long *args, int *err) {
	long r = syscall(SYS_socketcall, call, args);
	*err = r < 0 ? errno : 0;
	return r;
}
