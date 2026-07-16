package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/antopolskiy/vault-rename/internal/apperr"
	"github.com/antopolskiy/vault-rename/internal/model"
)

func TestLoadDefaultsAndStrictConfig(t *testing.T) {
	root := t.TempDir()
	cfg, _, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backlinks != model.BacklinksRepair {
		t.Fatalf("backlinks = %q", cfg.Backlinks)
	}

	path := filepath.Join(root, FileName)
	if err := os.WriteFile(path, []byte("version = 1\nunknown = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = Load(root, "")
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeConfigError {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateRejectsEachInvalidMode(t *testing.T) {
	tests := []Config{
		{Version: 2, Backlinks: model.BacklinksRepair, UnsupportedReferences: model.UnsupportedError, FrontmatterTitle: model.FrontmatterTitleExact},
		{Version: 1, Backlinks: "guess", UnsupportedReferences: model.UnsupportedError, FrontmatterTitle: model.FrontmatterTitleExact},
		{Version: 1, Backlinks: model.BacklinksRepair, UnsupportedReferences: "ignore", FrontmatterTitle: model.FrontmatterTitleExact},
		{Version: 1, Backlinks: model.BacklinksRepair, UnsupportedReferences: model.UnsupportedError, FrontmatterTitle: "always"},
	}
	for _, cfg := range tests {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected invalid config: %#v", cfg)
		}
	}
}
