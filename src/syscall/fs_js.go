// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build js

// This file is derived from Go's src/syscall/fs_js.go. File calls reach the
// host's globalThis.fs (Node's fs, or a page filesystem such as bottle's
// jsfs) through its callback API and park the goroutine until the callback
// runs, so a filesystem that answers later, like a mount backed by the
// network, works. wasi-libc's synchronous WASI imports cannot wait for one.

package syscall

import (
	"sync"
	"syscall/js"
	"unsafe"
)

var jsProcess = js.Global().Get("process")
var jsFS = js.Global().Get("fs")
var constants = jsFS.Get("constants")

var uint8Array = js.Global().Get("Uint8Array")

var (
	nodeWRONLY    = constants.Get("O_WRONLY").Int()
	nodeRDWR      = constants.Get("O_RDWR").Int()
	nodeCREATE    = constants.Get("O_CREAT").Int()
	nodeTRUNC     = constants.Get("O_TRUNC").Int()
	nodeAPPEND    = constants.Get("O_APPEND").Int()
	nodeEXCL      = constants.Get("O_EXCL").Int()
	nodeDIRECTORY = -1
)

func init() {
	oDir := constants.Get("O_DIRECTORY")
	if !oDir.IsUndefined() {
		nodeDIRECTORY = oDir.Int()
	}
}

type jsFile struct {
	path   string
	pos    int64
	seeked bool
}

var filesMu sync.Mutex
var files = map[int]*jsFile{
	0: {},
	1: {},
	2: {},
}

func fdToFile(fd int) (*jsFile, error) {
	filesMu.Lock()
	f, ok := files[fd]
	filesMu.Unlock()
	if !ok {
		return nil, EBADF
	}
	return f, nil
}

func Open(path string, openmode int, perm uint32) (int, error) {
	if err := checkPath(path); err != nil {
		return 0, err
	}

	flags := 0
	switch openmode & O_RDWR {
	case O_WRONLY:
		flags |= nodeWRONLY
	case O_RDWR:
		flags |= nodeRDWR
	}
	if openmode&O_CREAT != 0 {
		flags |= nodeCREATE
	}
	if openmode&O_TRUNC != 0 {
		flags |= nodeTRUNC
	}
	if openmode&O_APPEND != 0 {
		flags |= nodeAPPEND
	}
	if openmode&O_EXCL != 0 {
		flags |= nodeEXCL
	}
	if openmode&O_DIRECTORY != 0 && nodeDIRECTORY != -1 {
		flags |= nodeDIRECTORY
	}

	jsFD, err := fsCall("open", path, flags, perm)
	if err != nil {
		return 0, err
	}
	fd := jsFD.Int()

	if openmode&O_DIRECTORY != 0 && nodeDIRECTORY == -1 {
		var st Stat_t
		if err := Fstat(fd, &st); err != nil || st.Mode&S_IFMT != S_IFDIR {
			fsCall("close", fd)
			if err == nil {
				err = ENOTDIR
			}
			return 0, err
		}
	}

	if len(path) == 0 || path[0] != '/' {
		if wd, err := Getwd(); err == nil {
			if wd == "/" {
				wd = ""
			}
			path = wd + "/" + path
		}
	}

	filesMu.Lock()
	files[fd] = &jsFile{path: path}
	filesMu.Unlock()
	return fd, nil
}

func Close(fd int) error {
	filesMu.Lock()
	delete(files, fd)
	filesMu.Unlock()
	_, err := fsCall("close", fd)
	return err
}

func Mkdir(path string, perm uint32) error {
	if err := checkPath(path); err != nil {
		return err
	}
	_, err := fsCall("mkdir", path, perm)
	return err
}

// jsDir is a directory stream from Fdopendir. Its handle is odd, so it never
// equals a pointer.
type jsDir struct {
	entries []string
	idx     int
}

var dirs = map[uintptr]*jsDir{}
var nextDir uintptr = 1

func Fdopendir(fd int) (dir uintptr, err error) {
	f, err := fdToFile(fd)
	if err != nil {
		return 0, err
	}
	list, err := fsCall("readdir", f.path)
	if err != nil {
		return 0, err
	}
	d := &jsDir{entries: make([]string, list.Length())}
	for i := range d.entries {
		d.entries[i] = list.Index(i).String()
	}
	filesMu.Lock()
	dir = nextDir
	nextDir += 2
	dirs[dir] = d
	filesMu.Unlock()
	return dir, nil
}

// Fdclosedir releases the stream and, like wasi-libc's, leaves fd open.
func Fdclosedir(dir uintptr) (err error) {
	filesMu.Lock()
	delete(dirs, dir)
	filesMu.Unlock()
	return nil
}

// Readdir returns the next entry, or nil at the end. Its type is unknown, so
// os takes it from an lstat.
func Readdir(dir uintptr) (dirent *Dirent, err error) {
	filesMu.Lock()
	d, ok := dirs[dir]
	filesMu.Unlock()
	if !ok {
		return nil, EBADF
	}
	if d.idx >= len(d.entries) {
		return nil, nil
	}
	name := d.entries[d.idx]
	d.idx++
	// Dirent.Name reads a NUL-terminated name laid out right after the struct.
	buf := make([]uint64, (9+len(name)+1+7)/8)
	b := unsafe.Slice((*byte)(unsafe.Pointer(&buf[0])), len(buf)*8)
	copy(b[9:], name)
	dirent = (*Dirent)(unsafe.Pointer(&buf[0]))
	dirent.Type = DT_UNKNOWN
	return dirent, nil
}

func num(v js.Value, name string) float64 {
	f := v.Get(name)
	if f.Type() != js.TypeNumber {
		return 0
	}
	return f.Float()
}

func setStat(st *Stat_t, jsSt js.Value) {
	*st = Stat_t{}
	st.Dev = uint64(num(jsSt, "dev"))
	st.Ino = uint64(num(jsSt, "ino"))
	st.Mode = uint32(num(jsSt, "mode"))
	st.Nlink = uint64(num(jsSt, "nlink"))
	st.Uid = uint32(int32(num(jsSt, "uid")))
	st.Gid = uint32(int32(num(jsSt, "gid")))
	st.Rdev = uint64(num(jsSt, "rdev"))
	st.Size = int64(num(jsSt, "size"))
	st.Blksize = int32(num(jsSt, "blksize"))
	st.Blocks = int64(num(jsSt, "blocks"))
	st.Atim = msToTimespec(num(jsSt, "atimeMs"))
	st.Mtim = msToTimespec(num(jsSt, "mtimeMs"))
	st.Ctim = msToTimespec(num(jsSt, "ctimeMs"))
}

func msToTimespec(ms float64) Timespec {
	t := int64(ms)
	return Timespec{Sec: int32(t / 1000), Nsec: (t % 1000) * 1000000}
}

func Stat(path string, st *Stat_t) error {
	if err := checkPath(path); err != nil {
		return err
	}
	jsSt, err := fsCall("stat", path)
	if err != nil {
		return err
	}
	setStat(st, jsSt)
	return nil
}

func Lstat(path string, st *Stat_t) error {
	if err := checkPath(path); err != nil {
		return err
	}
	jsSt, err := fsCall("lstat", path)
	if err != nil {
		return err
	}
	setStat(st, jsSt)
	return nil
}

func Fstat(fd int, st *Stat_t) error {
	jsSt, err := fsCall("fstat", fd)
	if err != nil {
		return err
	}
	setStat(st, jsSt)
	return nil
}

func Unlink(path string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	_, err := fsCall("unlink", path)
	return err
}

func Rmdir(path string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	_, err := fsCall("rmdir", path)
	return err
}

func Chmod(path string, mode uint32) error {
	if err := checkPath(path); err != nil {
		return err
	}
	_, err := fsCall("chmod", path, mode)
	return err
}

func Chown(path string, uid, gid int) error {
	if err := checkPath(path); err != nil {
		return err
	}
	_, err := fsCall("chown", path, uint32(uid), uint32(gid))
	return err
}

func Lchown(path string, uid, gid int) error {
	if err := checkPath(path); err != nil {
		return err
	}
	if jsFS.Get("lchown").IsUndefined() {
		return ENOSYS
	}
	_, err := fsCall("lchown", path, uint32(uid), uint32(gid))
	return err
}

func Rename(from, to string) error {
	if err := checkPath(from); err != nil {
		return err
	}
	if err := checkPath(to); err != nil {
		return err
	}
	_, err := fsCall("rename", from, to)
	return err
}

func Truncate(path string, length int64) error {
	if err := checkPath(path); err != nil {
		return err
	}
	_, err := fsCall("truncate", path, length)
	return err
}

// Getwd and Chdir use the host's process, whose directory is the one the
// filesystem resolves relative paths against.
func Getwd() (string, error) {
	cwd := jsProcess.Get("cwd")
	if cwd.Type() != js.TypeFunction {
		return "", ENOSYS
	}
	return jsProcess.Call("cwd").String(), nil
}

func Chdir(path string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	// process.chdir throws, which Go cannot catch without recover, so the
	// path is checked first.
	var st Stat_t
	if err := Stat(path, &st); err != nil {
		return err
	}
	if st.Mode&S_IFMT != S_IFDIR {
		return ENOTDIR
	}
	if jsProcess.Get("chdir").Type() != js.TypeFunction {
		return ENOSYS
	}
	jsProcess.Call("chdir", path)
	return nil
}

func Readlink(path string, buf []byte) (n int, err error) {
	if err := checkPath(path); err != nil {
		return 0, err
	}
	dst, err := fsCall("readlink", path)
	if err != nil {
		return 0, err
	}
	n = copy(buf, dst.String())
	return n, nil
}

func Link(path, link string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	if err := checkPath(link); err != nil {
		return err
	}
	_, err := fsCall("link", path, link)
	return err
}

func Symlink(path, link string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	if err := checkPath(link); err != nil {
		return err
	}
	_, err := fsCall("symlink", path, link)
	return err
}

func Fsync(fd int) error {
	_, err := fsCall("fsync", fd)
	return err
}

func Read(fd int, b []byte) (int, error) {
	f, err := fdToFile(fd)
	if err != nil {
		return 0, err
	}

	if f.seeked {
		n, err := Pread(fd, b, f.pos)
		f.pos += int64(n)
		return n, err
	}

	buf := uint8Array.New(len(b))
	n, err := fsCall("read", fd, buf, 0, len(b), nil)
	if err != nil {
		return 0, err
	}
	n2 := n.Int()
	if n2 > 0 {
		js.CopyBytesToGo(b[:n2], buf)
	}

	f.pos += int64(n2)
	return n2, err
}

func Write(fd int, b []byte) (int, error) {
	f, err := fdToFile(fd)
	if err != nil {
		return 0, err
	}

	if f.seeked {
		n, err := Pwrite(fd, b, f.pos)
		f.pos += int64(n)
		return n, err
	}

	buf := uint8Array.New(len(b))
	js.CopyBytesToJS(buf, b)
	n, err := fsCall("write", fd, buf, 0, len(b), nil)
	if err != nil {
		return 0, err
	}
	n2 := n.Int()
	f.pos += int64(n2)
	return n2, err
}

func Pread(fd int, b []byte, offset int64) (int, error) {
	buf := uint8Array.New(len(b))
	n, err := fsCall("read", fd, buf, 0, len(b), offset)
	if err != nil {
		return 0, err
	}
	n2 := n.Int()
	if n2 > 0 {
		js.CopyBytesToGo(b[:n2], buf)
	}
	return n2, nil
}

func Pwrite(fd int, b []byte, offset int64) (int, error) {
	buf := uint8Array.New(len(b))
	js.CopyBytesToJS(buf, b)
	n, err := fsCall("write", fd, buf, 0, len(b), offset)
	if err != nil {
		return 0, err
	}
	return n.Int(), nil
}

func Seek(fd int, offset int64, whence int) (int64, error) {
	f, err := fdToFile(fd)
	if err != nil {
		return 0, err
	}

	var newPos int64
	switch whence {
	case 0:
		newPos = offset
	case 1:
		newPos = f.pos + offset
	case 2:
		var st Stat_t
		if err := Fstat(fd, &st); err != nil {
			return 0, err
		}
		newPos = st.Size + offset
	default:
		return 0, EINVAL
	}

	if newPos < 0 {
		return 0, EINVAL
	}

	f.seeked = true
	f.pos = newPos
	return newPos, nil
}

func Dup(fd int) (int, error) {
	return 0, ENOSYS
}

func fsCall(name string, args ...any) (js.Value, error) {
	type callResult struct {
		val js.Value
		err error
	}

	c := make(chan callResult, 1)
	f := js.FuncOf(func(this js.Value, args []js.Value) any {
		var res callResult

		if len(args) >= 1 {
			if jsErr := args[0]; !jsErr.IsNull() && !jsErr.IsUndefined() {
				res.err = mapJSError(jsErr)
			}
		}

		res.val = js.Undefined()
		if len(args) >= 2 {
			res.val = args[1]
		}

		c <- res
		return nil
	})
	defer f.Release()
	jsFS.Call(name, append(args, f)...)
	res := <-c
	return res.val, res.err
}

// checkPath checks that the path is not empty and that it contains no null characters.
func checkPath(path string) error {
	if path == "" {
		return EINVAL
	}
	for i := 0; i < len(path); i++ {
		if path[i] == '\x00' {
			return EINVAL
		}
	}
	return nil
}

var errnoByCode = map[string]Errno{
	"E2BIG":        E2BIG,
	"EACCES":       EACCES,
	"EAGAIN":       EAGAIN,
	"EBADF":        EBADF,
	"EBUSY":        EBUSY,
	"EEXIST":       EEXIST,
	"EFAULT":       EFAULT,
	"EFBIG":        EFBIG,
	"EINTR":        EINTR,
	"EINVAL":       EINVAL,
	"EIO":          EIO,
	"EISDIR":       EISDIR,
	"ELOOP":        ELOOP,
	"EMFILE":       EMFILE,
	"ENAMETOOLONG": ENAMETOOLONG,
	"ENODEV":       ENODEV,
	"ENOENT":       ENOENT,
	"ENOMEM":       ENOMEM,
	"ENOSPC":       ENOSPC,
	"ENOSYS":       ENOSYS,
	"ENOTDIR":      ENOTDIR,
	"ENOTEMPTY":    ENOTEMPTY,
	"ENOTSUP":      ENOTSUP,
	"EOPNOTSUPP":   EOPNOTSUPP,
	"ENOTTY":       ENOTTY,
	"EPERM":        EPERM,
	"EPIPE":        EPIPE,
	"EROFS":        EROFS,
	"ESPIPE":       ESPIPE,
	"EXDEV":        EXDEV,
}

// mapJSError maps an error from the host's fs to an Errno. Go panics on a
// code it does not know; an unknown one is EIO here.
func mapJSError(jsErr js.Value) error {
	code := jsErr.Get("code")
	if code.Type() == js.TypeString {
		if errno, ok := errnoByCode[code.String()]; ok {
			return errno
		}
	}
	return EIO
}
