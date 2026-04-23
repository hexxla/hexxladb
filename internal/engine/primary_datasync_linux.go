//go:build linux

package engine

import (
	"os"

	"golang.org/x/sys/unix"
)

func primaryDataSyncFile(f *os.File) error {
	//nolint:gosec // G115 — kernel FDs from *os.File match int on Linux targets we support.
	return unix.Fdatasync(int(f.Fd()))
}
