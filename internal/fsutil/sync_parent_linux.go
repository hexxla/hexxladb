//go:build linux

package fsutil

import (
	"errors"
	"os"
)

func syncDirectory(path string) (retErr error) {
	// #nosec G304 -- path is a parent directory derived from an application-owned database component.
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, directory.Close())
	}()
	return directory.Sync()
}
