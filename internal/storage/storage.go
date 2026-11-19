package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrNotFound      = errors.New("document not found")
	ErrAlreadyExists = errors.New("document already exists")
)

type Document struct {
	ID         string    `json:"id"`
	Filename   string    `json:"filename"`
	Size       int64     `json:"size"`
	MimeType   string    `json:"mime_type"`
	CreatedAt  time.Time `json:"created_at"`
	ModifiedAt time.Time `json:"modified_at"`
	Refs       []string  `json:"refs,omitempty"`
}

type Store interface {
	Create(filename string, data []byte) (*Document, error)
	Get(id string) (*Document, error)
	GetData(id string) ([]byte, error)
	List() ([]Document, error)
	Delete(id string) error
	Update(id string, data []byte) (*Document, error)
	AddRef(id, refID string) error
	GetRefs(id string) ([]string, error)
}

// Filesystem hardening limits shared by all file handlers.
const (
	MaxWriteBytes       = int64(64 << 20) // 64 MiB per write
	MaxReadBytes        = int64(32 << 20) // 32 MiB per read
	MaxWalkEntries      = 10000           // max entries per directory walk
	MaxZipEntries       = 1000            // max entries extracted per zip
	MaxZipUncompressed  = int64(256 << 20)
	MaxCreateZipSources = 1000
	// backupTimeFormat has nanosecond resolution so two overwrites of the
	// same file inside one second still get distinct .bak- names (writeBackup
	// refuses an existing destination with O_EXCL).
	backupTimeFormat = "20060102150405.000000000"
)

// ResolvePath absolutizes and cleans p, rejects NUL bytes, resolves symlinks
// on the deepest existing ancestor, and requires the result to stay inside one
// of roots. It follows a leaf symlink (for reads); write/delete targets must
// use ResolveDest instead so a leaf symlink is never followed.
func ResolvePath(p string, roots []string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("invalid path: contains NUL byte")
	}
	abs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	// Deepest existing ancestor via Lstat (never follows symlinks here).
	cur := abs
	var rest []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("path outside allowed roots: %s", p)
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
	real, err := filepath.EvalSymlinks(cur)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	resolved := filepath.Clean(filepath.Join(append([]string{real}, rest...)...))
	if !insideAnyRoot(resolved, roots) {
		return "", fmt.Errorf("path outside allowed roots: %s", p)
	}
	return resolved, nil
}

// ResolveDest resolves p for creation/write/delete: the parent directory must
// resolve inside a root, and a leaf symlink is never followed (refused).
func ResolveDest(p string, roots []string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("invalid path: contains NUL byte")
	}
	abs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	parent, err := ResolvePath(filepath.Dir(abs), roots)
	if err != nil {
		if strings.HasPrefix(err.Error(), "path outside allowed roots:") {
			return "", fmt.Errorf("path outside allowed roots: %s", p)
		}
		return "", err
	}
	full := filepath.Join(parent, filepath.Base(abs))
	if !insideAnyRoot(full, roots) {
		return "", fmt.Errorf("path outside allowed roots: %s", p)
	}
	if fi, err := os.Lstat(full); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing to follow symlink: %s", p)
	}
	return full, nil
}

// canonicalRoot absolutizes and cleans r, resolving a symlinked root itself.
// It reports false for NUL-containing or unresolvable roots.
func canonicalRoot(r string) (string, bool) {
	if strings.ContainsRune(r, 0) {
		return "", false
	}
	abs, err := filepath.Abs(filepath.Clean(r))
	if err != nil {
		return "", false
	}
	// Resolve the root itself so a symlinked root still matches.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return abs, true
}

func insideAnyRoot(resolved string, roots []string) bool {
	for _, r := range roots {
		abs, ok := canonicalRoot(r)
		if !ok {
			continue
		}
		if resolved == abs || strings.HasPrefix(resolved, abs+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// IsAllowedRoot reports whether the already-resolved absolute path is exactly
// one of the configured allowed roots, comparing both sides after Clean and
// a best-effort EvalSymlinks (roots reuse the insideAnyRoot resolution).
func IsAllowedRoot(resolved string, roots []string) bool {
	cleaned := filepath.Clean(resolved)
	if real, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = real
	}
	for _, r := range roots {
		abs, ok := canonicalRoot(r)
		if !ok {
			continue
		}
		if cleaned == abs {
			return true
		}
	}
	return false
}

// BackupTimestamp returns the current time formatted for .bak- suffixes.
func BackupTimestamp() string {
	return time.Now().Format(backupTimeFormat)
}
