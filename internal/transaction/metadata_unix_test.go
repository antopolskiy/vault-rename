//go:build darwin || linux

package transaction

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

func TestAtomicWritePreservesModeAndExtendedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("before"), 0o640); err != nil { //nolint:gosec // the test verifies preservation of group-readable mode.
		t.Fatal(err)
	}
	name := "user.vault-rename-test"
	if runtime.GOOS == "darwin" {
		name = "com.example.vault-rename-test"
	}
	if err := unix.Setxattr(path, name, []byte("kept"), 0); err != nil {
		t.Skipf("filesystem does not support test xattrs: %v", err)
	}
	if err := atomicWrite(path, []byte("after"), 0o640, "test", nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is owned by this temporary test directory.
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "after" {
		t.Fatalf("data = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	size, err := unix.Getxattr(path, name, nil)
	if err != nil {
		t.Fatal(err)
	}
	value := make([]byte, size)
	size, err = unix.Getxattr(path, name, value)
	if err != nil {
		t.Fatal(err)
	}
	if string(value[:size]) != "kept" {
		t.Fatalf("xattr = %q", value[:size])
	}
}
