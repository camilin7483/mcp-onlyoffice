//go:build linux || darwin

package storage

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryFileLock(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) {
		return true, nil
	}
	return false, err
}

func replacePath(from, to string) error {
	return os.Rename(from, to)
}

func syncMetadataDirectory(dir string) error {
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func releaseFileLock(file *os.File) {
	_ = file.Close()
}
