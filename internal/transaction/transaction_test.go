package transaction

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/antopolskiy/vault-rename/internal/state"
)

func TestPendingAndManifestValidation(t *testing.T) {
	recovery := filepath.Join(t.TempDir(), "recovery")
	paths := state.Paths{Recovery: recovery}
	pending, err := Pending(paths)
	if err != nil || pending {
		t.Fatalf("Pending() = %v, %v", pending, err)
	}
	dir := filepath.Join(recovery, "operation")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pending, err = Pending(paths)
	if err != nil || !pending {
		t.Fatalf("Pending() = %v, %v", pending, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManifest(dir); err == nil {
		t.Fatal("expected invalid manifest error")
	}
}

func TestExactExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Exact.md")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !exactExists(path) {
		t.Fatal("exact file was not found")
	}
	if exactExists(filepath.Join(dir, "Missing.md")) {
		t.Fatal("missing file was found")
	}
}
