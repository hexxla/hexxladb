//go:build aix || illumos || js || plan9 || wasip1

package engine

import "os"

func lockDatabaseFile(_ *os.File) error {
	return ErrFileLockUnsupported
}
