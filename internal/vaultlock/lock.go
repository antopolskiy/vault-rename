package vaultlock

import (
	"context"
	"time"

	"github.com/gofrs/flock"

	"github.com/antopolskiy/vault-rename/internal/apperr"
)

type Lock struct {
	file *flock.Flock
}

func Acquire(path string) (*Lock, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	file := flock.New(path)
	ok, err := file.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeIOError, "cannot acquire vault lock", err)
	}
	if !ok {
		return nil, apperr.New(apperr.CodeVaultBusy, "another vault rename operation is active")
	}
	return &Lock{file: file}, nil
}

func (l *Lock) Close() error {
	if err := l.file.Unlock(); err != nil {
		return apperr.Wrap(apperr.CodeIOError, "cannot release vault lock", err)
	}
	return nil
}
