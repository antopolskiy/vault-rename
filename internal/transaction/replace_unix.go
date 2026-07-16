//go:build !windows

package transaction

import "os"

func replaceFile(temp, target string) error {
	return os.Rename(temp, target) //nolint:gosec // both paths are internal validated transaction paths.
}

func syncDirectory(path string) error {
	dir, err := os.Open(path) //nolint:gosec // path is a validated parent directory of a transaction file.
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
