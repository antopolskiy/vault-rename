//go:build windows

package transaction

import "io/fs"

func hasMultipleLinks(_ fs.FileInfo) bool {
	return false
}
