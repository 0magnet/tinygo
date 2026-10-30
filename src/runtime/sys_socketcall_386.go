//go:build 386 && linux && !baremetal

package runtime

// golang.org/x/net/internal/socket declares socketcall and rawsocketcall
// without bodies and expects Go assembly, which TinyGo never assembles. The
// body lives in socketcall_linux_386.c. The second result is a syscall.Errno.

//export tinygo_socketcall
func tinygo_socketcall(call uintptr, args *uintptr, err *int32) uintptr

//go:linkname netSocketcall golang.org/x/net/internal/socket.socketcall
func netSocketcall(call, a0, a1, a2, a3, a4, a5 uintptr) (uintptr, uintptr) {
	args := [6]uintptr{a0, a1, a2, a3, a4, a5}
	var err int32
	r := tinygo_socketcall(call, &args[0], &err)
	return r, uintptr(err)
}

//go:linkname netRawsocketcall golang.org/x/net/internal/socket.rawsocketcall
func netRawsocketcall(call, a0, a1, a2, a3, a4, a5 uintptr) (uintptr, uintptr) {
	return netSocketcall(call, a0, a1, a2, a3, a4, a5)
}
