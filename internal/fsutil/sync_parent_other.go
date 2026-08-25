//go:build !linux

package fsutil

// HexxlaDB's production-readiness profile is Linux-only. Other supported Go
// build targets retain their existing file-sync behavior.
func syncDirectory(string) error {
	return nil
}
