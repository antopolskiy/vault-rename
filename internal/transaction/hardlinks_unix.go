//go:build !windows

package transaction

import (
	"io/fs"
	"syscall"
)

func hasMultipleLinks(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink > 1
}
