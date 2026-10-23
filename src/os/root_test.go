//go:build darwin || (linux && !baremetal && !js && !wasip1 && !wasip2 && !386 && !arm)

package os_test

import (
	. "os"
	"testing"
)

func TestRootConfinesNames(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFile(dir+"/a.pem", []byte("pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if b, err := r.ReadFile("a.pem"); err != nil || string(b) != "pem" {
		t.Fatalf("ReadFile(a.pem) = %q, %v", b, err)
	}
	if err := r.Mkdir("sub", 0o700); err != nil {
		t.Fatal(err)
	}
	if b, err := r.ReadFile("sub/../a.pem"); err != nil || string(b) != "pem" {
		t.Fatalf("ReadFile(sub/../a.pem) = %q, %v", b, err)
	}
	for _, name := range []string{"../a.pem", "/etc/passwd", "sub/../../a.pem", ""} {
		if _, err := r.ReadFile(name); err == nil {
			t.Errorf("ReadFile(%q) escaped the root", name)
		}
	}
	if _, err := OpenRoot(dir + "/a.pem"); err == nil {
		t.Error("OpenRoot of a file succeeded")
	}
	r.Close()
	if _, err := r.ReadFile("a.pem"); err == nil {
		t.Error("ReadFile after Close succeeded")
	}
}
