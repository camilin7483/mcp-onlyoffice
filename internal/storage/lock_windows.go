package storage

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func tryFileLock(file *os.File) (bool, error) {
	var offset windows.Overlapped
	err := windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &offset)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return true, nil
	}
	return false, err
}

func replacePath(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, target, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func syncMetadataDirectory(_ string) error {
	// replacePath solicita la persistencia del renombrado a Windows.
	return nil
}

func releaseFileLock(file *os.File) {
	var offset windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &offset)
	_ = file.Close()
}
