package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	LockFilename     = ".mcp-onlyoffice.lock"
	ToolLockFilename = ".mcp-tool-writes.lock"
)

// LockDirectory coordina instancias cooperantes, incluso si reemplazan los archivos por renombrado.
func LockDirectory(ctx context.Context, dir string) (func(), error) {
	return lockNamedFile(ctx, dir, LockFilename)
}

// LockToolWrites coordina herramientas que pueden tocar varios archivos.
func LockToolWrites(ctx context.Context, dir string) (func(), error) {
	return lockNamedFile(ctx, dir, ToolLockFilename)
}

func lockNamedFile(ctx context.Context, dir, name string) (func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("invalid lock file: %s", path)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open write lock: %w", err)
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		busy, err := tryFileLock(file)
		if err != nil {
			file.Close()
			return nil, fmt.Errorf("acquire write lock: %w", err)
		}
		if !busy {
			return func() { releaseFileLock(file) }, nil
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, fmt.Errorf("directory write lock is busy: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// CheckDesktopWrite comprueba la marca que Desktop Editors instala al abrir para edición.
func CheckDesktopWrite(path string) error {
	marker := filepath.Join(filepath.Dir(path), ".~lock."+filepath.Base(path)+"#")
	if _, err := os.Lstat(marker); err == nil {
		return fmt.Errorf("document is locked by an editor: close the editor before writing")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check editor lock: %w", err)
	}
	return nil
}
