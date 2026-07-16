//go:build darwin || linux

package transaction

import (
	"bytes"
	"errors"

	"golang.org/x/sys/unix"
)

func copyExtendedMetadata(source, destination string) error {
	size, err := unix.Listxattr(source, nil)
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
		return nil
	}
	if err != nil || size == 0 {
		return err
	}
	names := make([]byte, size)
	size, err = unix.Listxattr(source, names)
	if err != nil {
		return err
	}
	for _, rawName := range bytes.Split(names[:size], []byte{0}) {
		if len(rawName) == 0 {
			continue
		}
		name := string(rawName)
		valueSize, err := unix.Getxattr(source, name, nil)
		if errors.Is(err, unix.ENODATA) {
			continue
		}
		if err != nil {
			return err
		}
		value := make([]byte, valueSize)
		valueSize, err = unix.Getxattr(source, name, value)
		if err != nil {
			return err
		}
		if err := unix.Setxattr(destination, name, value[:valueSize], 0); err != nil {
			return err
		}
	}
	return nil
}
