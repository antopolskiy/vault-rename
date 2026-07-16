package vaultlock

import (
	"path/filepath"
	"testing"
)

func TestAcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rename.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}
