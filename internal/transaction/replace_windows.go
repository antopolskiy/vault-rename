//go:build windows

package transaction

import "golang.org/x/sys/windows"

func replaceFile(temp, target string) error {
	tempPtr, err := windows.UTF16PtrFromString(temp)
	if err != nil {
		return err
	}
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		tempPtr,
		targetPtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}

func syncDirectory(_ string) error {
	return nil
}
