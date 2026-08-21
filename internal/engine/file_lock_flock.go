//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package engine

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lockDatabaseFile(f *os.File) error {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, unix.EINTR):
			continue
		case errors.Is(err, unix.EWOULDBLOCK), errors.Is(err, unix.EAGAIN):
			return ErrDatabaseLocked
		default:
			return fmt.Errorf("engine: lock database file: %w", err)
		}
	}
}
