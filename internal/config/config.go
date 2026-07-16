package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/antopolskiy/vault-rename/internal/apperr"
	"github.com/antopolskiy/vault-rename/internal/model"
)

const FileName = ".vault-rename.toml"

type Config struct {
	Version               int                        `toml:"version"`
	Backlinks             model.BacklinkMode         `toml:"backlinks"`
	UnsupportedReferences model.UnsupportedMode      `toml:"unsupported_references"`
	FrontmatterTitle      model.FrontmatterTitleMode `toml:"frontmatter_title"`
	LogPath               string                     `toml:"log_path"`
	RecoveryPath          string                     `toml:"recovery_path"`
}

func Defaults() Config {
	return Config{
		Version:               1,
		Backlinks:             model.BacklinksRepair,
		UnsupportedReferences: model.UnsupportedError,
		FrontmatterTitle:      model.FrontmatterTitleExact,
	}
}

func Load(root, override string) (Config, string, error) {
	cfg := Defaults()
	path := override
	explicit := override != ""
	if path == "" {
		path = filepath.Join(root, FileName)
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)

	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) && !explicit {
		return cfg, path, nil
	}
	if err != nil {
		return Config{}, path, apperr.Wrap(apperr.CodeConfigError, "cannot open configuration", err)
	}
	defer file.Close()

	decoder := toml.NewDecoder(file).DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, path, apperr.Wrap(apperr.CodeConfigError, "invalid configuration", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, path, err
	}
	return cfg, path, nil
}

func (c Config) Validate() error {
	if c.Version != 1 {
		return apperr.New(apperr.CodeConfigError, "unsupported configuration version")
	}
	switch c.Backlinks {
	case model.BacklinksRepair, model.BacklinksCheck, model.BacklinksOff:
	default:
		return apperr.New(apperr.CodeConfigError, "backlinks must be repair, check, or off")
	}
	switch c.UnsupportedReferences {
	case model.UnsupportedError, model.UnsupportedWarn:
	default:
		return apperr.New(apperr.CodeConfigError, "unsupported_references must be error or warn")
	}
	switch c.FrontmatterTitle {
	case model.FrontmatterTitleExact, model.FrontmatterTitleNever:
	default:
		return apperr.New(apperr.CodeConfigError, "frontmatter_title must be exact-match or never")
	}
	return nil
}
