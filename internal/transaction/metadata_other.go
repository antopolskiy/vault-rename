//go:build !darwin && !linux

package transaction

func copyExtendedMetadata(_, _ string) error {
	return nil
}
