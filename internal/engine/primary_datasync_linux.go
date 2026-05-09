//go:build linux

package engine

import (
	"os"

	"golang.org/x/sys/unix"
)

func primaryDataSyncFile(f *os.File) error {
	return unix.Fdatasync(int(f.Fd()))
}
