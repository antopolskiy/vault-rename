package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/antopolskiy/vault-rename/internal/config"
)

func TestResolveAndEnsure(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	cfg := config.Defaults()
	cfg.LogPath = ".private/rename.sqlite3"
	cfg.RecoveryPath = ".private/recovery"

	paths, err := Resolve(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if paths.VaultID == "" || paths.Log != filepath.Join(root, ".private", "rename.sqlite3") {
		t.Fatalf("paths = %#v", paths)
	}
	if err := Ensure(paths); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.Base, filepath.Dir(paths.Log), paths.Recovery} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s permissions = %o", path, info.Mode().Perm())
		}
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if got := expandHome("~/state"); got != filepath.Join(home, "state") {
		t.Fatalf("expandHome = %q", got)
	}
	if runtime.GOOS == "windows" {
		if got := expandHome(`~\state`); got != filepath.Join(home, "state") {
			t.Fatalf("expandHome with Windows separator = %q", got)
		}
	}
	if got := expandHome("/absolute"); got != "/absolute" {
		t.Fatalf("absolute changed to %q", got)
	}
}
