package state

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/antopolskiy/vault-rename/internal/apperr"
	"github.com/antopolskiy/vault-rename/internal/config"
)

type Paths struct {
	VaultID  string
	Base     string
	Log      string
	Lock     string
	Recovery string
}

func Resolve(root string, cfg config.Config) (Paths, error) {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Paths{}, apperr.Wrap(apperr.CodeIOError, "cannot canonicalize vault root", err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return Paths{}, apperr.Wrap(apperr.CodeIOError, "cannot resolve vault root", err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(canonical)))
	id := hex.EncodeToString(sum[:])[:16]

	base, err := defaultBase()
	if err != nil {
		return Paths{}, err
	}
	base = filepath.Join(base, "vault-rename", "vaults", id)
	logPath := resolveConfigured(root, cfg.LogPath, filepath.Join(base, "renames.sqlite3"))
	recoveryPath := resolveConfigured(root, cfg.RecoveryPath, filepath.Join(base, "recovery"))
	return Paths{
		VaultID:  id,
		Base:     base,
		Log:      logPath,
		Lock:     filepath.Join(base, "rename.lock"),
		Recovery: recoveryPath,
	}, nil
}

func Ensure(paths Paths) error {
	for _, path := range []string{paths.Base, filepath.Dir(paths.Log), paths.Recovery} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return apperr.Wrap(apperr.CodeIOError, "cannot create state directory", err)
		}
		if err := os.Chmod(path, 0o700); err != nil { //nolint:gosec // owner-only directory permissions are intentional.
			return apperr.Wrap(apperr.CodeIOError, "cannot secure state directory", err)
		}
	}
	return nil
}

func defaultBase() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", apperr.Wrap(apperr.CodeIOError, "cannot determine home directory", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support"), nil
	case "windows":
		if value := os.Getenv("LOCALAPPDATA"); value != "" {
			return value, nil
		}
		return filepath.Join(home, "AppData", "Local"), nil
	default:
		if value := os.Getenv("XDG_STATE_HOME"); value != "" {
			return value, nil
		}
		return filepath.Join(home, ".local", "state"), nil
	}
}

func resolveConfigured(root, value, fallback string) string {
	if value == "" {
		return filepath.Clean(fallback)
	}
	value = expandHome(value)
	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}
	return filepath.Clean(value)
}

func expandHome(value string) string {
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, value[2:])
		}
	}
	return value
}
