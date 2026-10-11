//go:build nintendoswitch || wasip1 || wasip2

// The file calls through libc. js has its own, in fs_js.go.

package syscall

import (
	"unsafe"
)

func Close(fd int) (err error) {
	if libc_close(int32(fd)) < 0 {
		err = getErrno()
	}
	return
}

func Dup(fd int) (fd2 int, err error) {
	fd2 = int(libc_dup(int32(fd)))
	if fd2 < 0 {
		err = getErrno()
	}
	return
}

// Read, Write, Pread, Pwrite are defined per-build-target so that the
// wasip1 cooperative-scheduler build can wrap the libc syscalls with a
// park-on-EAGAIN loop. See syscall_libc_default.go and
// syscall_libc_wasip1.go.

func Seek(fd int, offset int64, whence int) (newoffset int64, err error) {
	newoffset = libc_lseek(int32(fd), offset, whence)
	if newoffset < 0 {
		err = getErrno()
	}
	return
}

func Open(path string, flag int, mode uint32) (fd int, err error) {
	data := cstring(path)
	fd = int(libc_open(&data[0], int32(flag), mode))
	if fd < 0 {
		err = getErrno()
	}
	return
}

func Fsync(fd int) (err error) {
	if libc_fsync(int32(fd)) < 0 {
		err = getErrno()
	}
	return
}

func Readlink(path string, p []byte) (n int, err error) {
	data := cstring(path)
	n = libc_readlink(&data[0], unsafe.SliceData(p), uint(len(p)))
	if n < 0 {
		err = getErrno()
	}
	return
}

func Chdir(path string) (err error) {
	data := cstring(path)
	fail := int(libc_chdir(&data[0]))
	if fail < 0 {
		err = getErrno()
	}
	return
}

func Mkdir(path string, mode uint32) (err error) {
	data := cstring(path)
	fail := int(libc_mkdir(&data[0], mode))
	if fail < 0 {
		err = getErrno()
	}
	return
}

func Rmdir(path string) (err error) {
	data := cstring(path)
	fail := int(libc_rmdir(&data[0]))
	if fail < 0 {
		err = getErrno()
	}
	return
}

func Rename(from, to string) (err error) {
	fromdata := cstring(from)
	todata := cstring(to)
	fail := int(libc_rename(&fromdata[0], &todata[0]))
	if fail < 0 {
		err = getErrno()
	}
	return
}

func Link(oldname, newname string) (err error) {
	fromdata := cstring(oldname)
	todata := cstring(newname)
	fail := int(libc_link(&fromdata[0], &todata[0]))
	if fail < 0 {
		err = getErrno()
	}
	return
}

func Symlink(from, to string) (err error) {
	fromdata := cstring(from)
	todata := cstring(to)
	fail := int(libc_symlink(&fromdata[0], &todata[0]))
	if fail < 0 {
		err = getErrno()
	}
	return
}

func Unlink(path string) (err error) {
	data := cstring(path)
	fail := int(libc_unlink(&data[0]))
	if fail < 0 {
		err = getErrno()
	}
	return
}

func Truncate(path string, length int64) (err error) {
	data := cstring(path)
	fail := int(libc_truncate(&data[0], length))
	if fail < 0 {
		err = getErrno()
	}
	return
}
