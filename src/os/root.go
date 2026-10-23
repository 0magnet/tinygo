package os

import (
	"errors"
	"io/fs"
	"strings"
	"time"
)

// TINYGO: Root confines names to a directory by checking them lexically, so
// a name with ".." or an absolute path cannot leave it. Unlike Go it does not
// open files relative to a held directory descriptor, so a symlink inside the
// directory can still point outside it.

// Root may be used to only access files within a single directory tree.
type Root struct {
	name   string
	closed bool
}

var errPathEscapes = errors.New("path escapes from parent")

// OpenRoot opens the named directory for use as a Root.
func OpenRoot(name string) (*Root, error) {
	fi, err := Stat(name)
	if err != nil {
		return nil, &PathError{Op: "openat", Path: name, Err: underlyingError(err)}
	}
	if !fi.IsDir() {
		return nil, &PathError{Op: "openat", Path: name, Err: errors.New("not a directory")}
	}
	return &Root{name: name}, nil
}

// OpenInRoot opens the file name in the directory dir.
func OpenInRoot(dir, name string) (*File, error) {
	r, err := OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Open(name)
}

// join returns the path of name under r, or an error if name leaves r.
func (r *Root) join(op, name string) (string, error) {
	if r.closed {
		return "", &PathError{Op: op, Path: name, Err: ErrClosed}
	}
	if name == "" || strings.HasPrefix(name, "/") {
		return "", &PathError{Op: op, Path: name, Err: errPathEscapes}
	}
	depth := 0
	for _, part := range strings.Split(name, "/") {
		switch part {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return "", &PathError{Op: op, Path: name, Err: errPathEscapes}
			}
		default:
			depth++
		}
	}
	return r.name + "/" + name, nil
}

// Name returns the name of the directory presented to OpenRoot.
func (r *Root) Name() string { return r.name }

// Close closes the Root.
func (r *Root) Close() error {
	r.closed = true
	return nil
}

// Open opens the named file in the root for reading.
func (r *Root) Open(name string) (*File, error) { return r.OpenFile(name, O_RDONLY, 0) }

// Create creates or truncates the named file in the root.
func (r *Root) Create(name string) (*File, error) {
	return r.OpenFile(name, O_RDWR|O_CREATE|O_TRUNC, 0o666)
}

// OpenFile opens the named file in the root.
func (r *Root) OpenFile(name string, flag int, perm FileMode) (*File, error) {
	p, err := r.join("openat", name)
	if err != nil {
		return nil, err
	}
	return OpenFile(p, flag, perm)
}

// OpenRoot opens the named directory in the root as another Root.
func (r *Root) OpenRoot(name string) (*Root, error) {
	p, err := r.join("openat", name)
	if err != nil {
		return nil, err
	}
	return OpenRoot(p)
}

// ReadFile reads the named file in the root.
func (r *Root) ReadFile(name string) ([]byte, error) {
	p, err := r.join("openat", name)
	if err != nil {
		return nil, err
	}
	return ReadFile(p)
}

// WriteFile writes data to the named file in the root.
func (r *Root) WriteFile(name string, data []byte, perm FileMode) error {
	p, err := r.join("openat", name)
	if err != nil {
		return err
	}
	return WriteFile(p, data, perm)
}

// Stat returns a FileInfo for the named file in the root.
func (r *Root) Stat(name string) (FileInfo, error) {
	p, err := r.join("statat", name)
	if err != nil {
		return nil, err
	}
	return Stat(p)
}

// Lstat returns a FileInfo for the named file in the root, not following a
// final symlink.
func (r *Root) Lstat(name string) (FileInfo, error) {
	p, err := r.join("lstatat", name)
	if err != nil {
		return nil, err
	}
	return Lstat(p)
}

// Mkdir creates a directory in the root.
func (r *Root) Mkdir(name string, perm FileMode) error {
	p, err := r.join("mkdirat", name)
	if err != nil {
		return err
	}
	return Mkdir(p, perm)
}

// MkdirAll creates a directory and any parents in the root.
func (r *Root) MkdirAll(name string, perm FileMode) error {
	p, err := r.join("mkdirat", name)
	if err != nil {
		return err
	}
	return MkdirAll(p, perm)
}

// Remove removes the named file or empty directory in the root.
func (r *Root) Remove(name string) error {
	p, err := r.join("removeat", name)
	if err != nil {
		return err
	}
	return Remove(p)
}

// RemoveAll removes the named file or directory and its contents in the root.
func (r *Root) RemoveAll(name string) error {
	p, err := r.join("removeat", name)
	if err != nil {
		return err
	}
	return RemoveAll(p)
}

// Rename renames oldname to newname, both in the root.
func (r *Root) Rename(oldname, newname string) error {
	op, err := r.join("renameat", oldname)
	if err != nil {
		return err
	}
	np, err := r.join("renameat", newname)
	if err != nil {
		return err
	}
	return Rename(op, np)
}

// Readlink returns the destination of the named symlink in the root.
func (r *Root) Readlink(name string) (string, error) {
	p, err := r.join("readlinkat", name)
	if err != nil {
		return "", err
	}
	return Readlink(p)
}

// Symlink creates newname in the root as a symlink to oldname.
func (r *Root) Symlink(oldname, newname string) error {
	p, err := r.join("symlinkat", newname)
	if err != nil {
		return err
	}
	return Symlink(oldname, p)
}

// Link creates newname in the root as a hard link to oldname in the root.
func (r *Root) Link(oldname, newname string) error {
	op, err := r.join("linkat", oldname)
	if err != nil {
		return err
	}
	np, err := r.join("linkat", newname)
	if err != nil {
		return err
	}
	return Link(op, np)
}

// Chmod changes the mode of the named file in the root.
func (r *Root) Chmod(name string, mode FileMode) error {
	p, err := r.join("chmodat", name)
	if err != nil {
		return err
	}
	return Chmod(p, mode)
}

// Chown changes the owner and group of the named file in the root.
func (r *Root) Chown(name string, uid, gid int) error {
	p, err := r.join("chownat", name)
	if err != nil {
		return err
	}
	return Chown(p, uid, gid)
}

// Lchown changes the owner and group of the named file in the root, not
// following a final symlink.
func (r *Root) Lchown(name string, uid, gid int) error {
	p, err := r.join("lchownat", name)
	if err != nil {
		return err
	}
	return Lchown(p, uid, gid)
}

// Chtimes changes the access and modification times of the named file in the
// root.
func (r *Root) Chtimes(name string, atime, mtime time.Time) error {
	p, err := r.join("chtimesat", name)
	if err != nil {
		return err
	}
	return Chtimes(p, atime, mtime)
}

// FS returns a file system for the tree of files in the root.
func (r *Root) FS() fs.FS { return DirFS(r.name) }
