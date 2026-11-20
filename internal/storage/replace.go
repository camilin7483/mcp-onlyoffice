package storage

import (
	"errors"
	"os"
	"path/filepath"
)

// ReplaceFile prepara y sincroniza el nuevo contenido antes de sustituir el nombre.
func ReplaceFile(path string, data []byte, mode os.FileMode, beforeCommit func() error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp-edit-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	_, writeErr := tmp.Write(data)
	syncErr := tmp.Sync()
	if err := errors.Join(writeErr, syncErr, tmp.Close()); err != nil {
		return err
	}
	if beforeCommit != nil {
		if err := beforeCommit(); err != nil {
			return err
		}
	}
	return replacePath(tmp.Name(), path)
}
