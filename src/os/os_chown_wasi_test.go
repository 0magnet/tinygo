//go:build wasip1 || wasip2

package os_test

import (
	"errors"
	"io/fs"
	. "os"
	"testing"
)

func TestChownWASI(t *testing.T) {
	f := newFile("TestChownWASI", t)
	defer Remove(f.Name())
	defer f.Close()

	if err := Chown(f.Name(), 1000, 1000); err != nil {
		t.Errorf("Chown: %v", err)
	}
	if err := Lchown(f.Name(), 1000, 1000); err != nil {
		t.Errorf("Lchown: %v", err)
	}
	if err := Chown(f.Name()+".missing", 1000, 1000); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Chown of a missing file: got %v, want ErrNotExist", err)
	}
}
