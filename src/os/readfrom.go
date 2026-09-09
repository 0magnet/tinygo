//go:build !baremetal

package os

import "io"

// ReadFrom copies from r into f until r returns io.EOF, implementing
// io.ReaderFrom.
//
// TINYGO: upstream Go defines this so that io.Copy into a File can use
// copy_file_range or sendfile where the platform allows. Nothing here does
// that, so this is the generic copy Go itself falls back to. It is worth
// having anyway: packages reference the method for its presence rather than
// its speed, and without it they fail to compile. fasthttp calls it on an
// *os.File, which is enough to stop fiber building for wasm.
//
// Deliberately not io.Copy(f, r): *File satisfies io.ReaderFrom through this
// very method, so the copy would call straight back into it.
func (f *File) ReadFrom(r io.Reader) (n int64, err error) {
	buf := make([]byte, 32*1024)
	for {
		nr, rerr := r.Read(buf)
		if nr > 0 {
			nw, werr := f.Write(buf[:nr])
			n += int64(nw)
			if werr != nil {
				return n, werr
			}
			if nw != nr {
				return n, io.ErrShortWrite
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return n, nil
			}
			return n, rerr
		}
	}
}
