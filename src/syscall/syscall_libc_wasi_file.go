//go:build wasip1 || wasip2

// The directory and stat calls through wasi-libc. js has its own, in
// fs_js.go.

package syscall

import "unsafe"

func Fdopendir(fd int) (dir uintptr, err error) {
	d := libc_fdopendir(int32(fd))

	if d == nil {
		err = getErrno()
	}
	return uintptr(d), err
}

func Fdclosedir(dir uintptr) (err error) {
	// Unlike on other unix platform where only closedir exists, wasi-libc has
	// fdclosedir which releases resources and returns the file descriptor but
	// does not close it. This is useful for us since we want to be able to keep
	// using it.
	n := libc_fdclosedir(unsafe.Pointer(dir))

	if n < 0 {
		err = getErrno()
	}
	return
}

func Readdir(dir uintptr) (dirent *Dirent, err error) {
	// There might be a leftover errno value in the global variable, so we have
	// to clear it before calling readdir because we cannot know whether a nil
	// return means that we reached EOF or that an error occurred.
	libcErrno = 0

	dirent = libc_readdir(unsafe.Pointer(dir))

	if dirent == nil && libcErrno != 0 {
		err = getErrno()
	}
	return
}

func Stat(path string, p *Stat_t) (err error) {
	data := cstring(path)
	n := libc_stat(&data[0], unsafe.Pointer(p))

	if n < 0 {
		err = getErrno()
	}
	return
}

func Fstat(fd int, p *Stat_t) (err error) {
	n := libc_fstat(int32(fd), unsafe.Pointer(p))

	if n < 0 {
		err = getErrno()
	}
	return
}

func Lstat(path string, p *Stat_t) (err error) {
	data := cstring(path)
	n := libc_lstat(&data[0], unsafe.Pointer(p))
	if n < 0 {
		err = getErrno()
	}
	return
}

func Chmod(path string, mode uint32) (err error) {
	// wasi does not have chmod, but there are tests that validate that calling
	// os.Chmod does not error (e.g. io/fs.TestIssue51617).
	//
	// We make a call to Lstat instead so we detect conditions like the path not
	// existing, but we don't honnor the request to modify the file permissions.
	stat := Stat_t{}
	return Lstat(path, &stat)
}

// Chown and Lchown only check that the path exists, as wasi has no file
// owners and wasi-libc has no chown.
func Chown(path string, uid, gid int) (err error) {
	stat := Stat_t{}
	return Stat(path, &stat)
}

func Lchown(path string, uid, gid int) (err error) {
	stat := Stat_t{}
	return Lstat(path, &stat)
}
