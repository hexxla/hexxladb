//go:build !linux && !darwin

package fsutil

// AvailableBytes reports that portable free-space information is unavailable.
// The production-readiness profile is Linux; callers on other platforms should
// perform an operator-specific capacity check.
func AvailableBytes(string) (uint64, bool, error) {
	return 0, false, nil
}

// FilesystemID reports that a portable filesystem identity is unavailable.
func FilesystemID(string) (string, bool, error) {
	return "", false, nil
}
