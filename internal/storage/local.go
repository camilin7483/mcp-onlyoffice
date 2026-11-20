package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type LocalStore struct {
	base   string
	metaFn string
}

// MaxDocBytes caps document payloads accepted by Create and Update.
const MaxDocBytes = 10 << 20

func NewLocalStore(base string) (*LocalStore, error) {
	abs, err := filepath.Abs(base)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	if err := os.MkdirAll(abs, 0755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}

	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve storage root: %w", err)
	}
	s := &LocalStore{
		base:   abs,
		metaFn: filepath.Join(abs, ".metadata.json"),
	}

	return s, nil
}

type metaEntry struct {
	ID         string    `json:"id"`
	Filename   string    `json:"filename"`
	Size       int64     `json:"size"`
	MimeType   string    `json:"mime_type"`
	CreatedAt  time.Time `json:"created_at"`
	ModifiedAt time.Time `json:"modified_at"`
	Refs       []string  `json:"refs,omitempty"`
	BlobExt    string    `json:"blob_ext,omitempty"`
}

func (s *LocalStore) dataPath(entry metaEntry) string {
	legacy := filepath.Join(s.base, entry.ID+".data")
	if entry.BlobExt != "" {
		path := filepath.Join(s.base, entry.ID+entry.BlobExt)
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return path
		}
	}
	return legacy
}

// BaseDir returns the absolute storage root (used to sandbox zip extraction).
func (s *LocalStore) BaseDir() string {
	return s.base
}

func (s *LocalStore) saveMeta(entries map[string]metaEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	tmp, err := os.CreateTemp(s.base, ".metadata-*")
	if err != nil {
		return fmt.Errorf("stage metadata: %w", err)
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(data)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if err := replacePath(tmp.Name(), s.metaFn); err != nil {
		return fmt.Errorf("replace metadata: %w", err)
	}
	return nil
}

func (s *LocalStore) Create(filename string, data []byte) (*Document, error) {
	unlock, err := LockDirectory(context.Background(), s.base)
	if err != nil {
		return nil, err
	}
	defer unlock()

	if len(data) > MaxDocBytes {
		return nil, fmt.Errorf("document payload %d bytes exceeds %d byte limit", len(data), MaxDocBytes)
	}
	id := uuid.NewString()
	now := time.Now()

	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}

	entry := metaEntry{
		ID:         id,
		Filename:   filename,
		Size:       int64(len(data)),
		MimeType:   mimeType(filename),
		CreatedAt:  now,
		ModifiedAt: now,
	}

	path := s.dataPath(entry)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("create payload: %w", err)
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	if err := errors.Join(writeErr, syncErr, file.Close()); err != nil {
		return nil, fmt.Errorf("write payload: %w", errors.Join(err, os.Remove(path)))
	}

	entries[id] = entry
	if err := s.saveMeta(entries); err != nil {
		return nil, errors.Join(err, os.Remove(s.dataPath(entry)))
	}

	return entryToDoc(entry), nil
}

func (s *LocalStore) Get(id string) (*Document, error) {
	unlock, err := LockDirectory(context.Background(), s.base)
	if err != nil {
		return nil, err
	}
	defer unlock()

	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}

	entry, ok := entries[id]
	if !ok {
		return nil, ErrNotFound
	}

	return s.currentDocument(entry), nil
}

func (s *LocalStore) GetData(id string) ([]byte, error) {
	unlock, err := LockDirectory(context.Background(), s.base)
	if err != nil {
		return nil, err
	}
	defer unlock()

	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}

	entry, ok := entries[id]
	if !ok {
		return nil, ErrNotFound
	}

	data, err := os.ReadFile(s.dataPath(entry))
	if err != nil {
		return nil, fmt.Errorf("read file data: %w", err)
	}

	return data, nil
}

func (s *LocalStore) List() ([]Document, error) {
	unlock, err := LockDirectory(context.Background(), s.base)
	if err != nil {
		return nil, err
	}
	defer unlock()

	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}

	docs := make([]Document, 0, len(entries))
	for _, e := range entries {
		docs = append(docs, *s.currentDocument(e))
	}

	sort.Slice(docs, func(i, j int) bool {
		return docs[i].CreatedAt.After(docs[j].CreatedAt)
	})

	return docs, nil
}

func (s *LocalStore) Delete(id string) error {
	unlock, err := LockDirectory(context.Background(), s.base)
	if err != nil {
		return err
	}
	defer unlock()

	entries, err := s.readEntries()
	if err != nil {
		return err
	}

	entry, ok := entries[id]
	if !ok {
		return ErrNotFound
	}

	path := s.dataPath(entry)
	if err := CheckDesktopWrite(path); err != nil {
		return err
	}
	// Primero se confirma la baja en el índice. Un fallo del índice conserva el archivo.
	delete(entries, id)
	if err := s.saveMeta(entries); err != nil {
		return err
	}
	if err := syncMetadataDirectory(s.base); err != nil {
		return fmt.Errorf("index replaced but could not be synced; payload retained at %s: %w", path, err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("document removed from index; payload retained at %s: %w", path, err)
	}
	if err := os.Remove(filepath.Join(s.base, id+".meta.json")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("document deleted; legacy sidecar retained: %w", err)
	}
	return nil
}

// DesktopPath conserva un solo archivo editable con la extensión que reconoce el editor.
func (s *LocalStore) DesktopPath(id string) (string, error) {
	unlock, err := LockDirectory(context.Background(), s.base)
	if err != nil {
		return "", err
	}
	defer unlock()
	entries, err := s.readEntries()
	if err != nil {
		return "", err
	}
	entry, ok := entries[id]
	if !ok {
		return "", ErrNotFound
	}
	current := s.dataPath(entry)
	info, err := os.Lstat(current)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("document file is not a regular file")
	}
	ext := entry.BlobExt
	if ext == "" {
		ext = strings.ToLower(filepath.Ext(entry.Filename))
		if !desktopExtension(ext) {
			return "", fmt.Errorf("unsupported Desktop document extension %q", ext)
		}
		dest := filepath.Join(s.base, id+ext)
		if _, err := os.Lstat(dest); !os.IsNotExist(err) {
			return "", fmt.Errorf("cannot reserve Desktop document path %q", dest)
		}
		// Si se interrumpe la migración, dataPath sigue leyendo el archivo original.
		entry.BlobExt = ext
		entries[id] = entry
		if err := s.saveMeta(entries); err != nil {
			return "", err
		}
	}
	dest := filepath.Join(s.base, id+ext)
	if current != dest {
		if err := os.Rename(current, dest); err != nil {
			return "", fmt.Errorf("move document to Desktop path: %w", err)
		}
	}
	return dest, nil
}

func desktopExtension(ext string) bool {
	switch ext {
	case ".docx", ".xlsx", ".pptx", ".pdf", ".doc", ".xls", ".ppt", ".txt", ".csv", ".odt", ".ods", ".odp", ".rtf", ".docm", ".xlsm", ".pptm":
		return true
	default:
		return false
	}
}

// Rename changes only the document metadata, preserving its ID and references.
func (s *LocalStore) Rename(id, filename string) (*Document, error) {
	unlock, err := LockDirectory(context.Background(), s.base)
	if err != nil {
		return nil, err
	}
	defer unlock()

	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}
	entry, ok := entries[id]
	if !ok {
		return nil, ErrNotFound
	}
	entry.Filename = filename
	entry.MimeType = mimeType(filename)
	entry.ModifiedAt = time.Now()
	entries[id] = entry
	if err := s.saveMeta(entries); err != nil {
		return nil, err
	}
	return s.currentDocument(entry), nil
}

func (s *LocalStore) Update(id string, data []byte) (*Document, error) {
	unlock, err := LockDirectory(context.Background(), s.base)
	if err != nil {
		return nil, err
	}
	defer unlock()

	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}

	entry, ok := entries[id]
	if !ok {
		return nil, ErrNotFound
	}

	if len(data) > MaxDocBytes {
		return nil, fmt.Errorf("document payload %d bytes exceeds %d byte limit", len(data), MaxDocBytes)
	}
	path := s.dataPath(entry)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("document file is not a regular file")
	}
	if err := ReplaceFile(path, data, info.Mode().Perm(), func() error {
		return CheckDesktopWrite(path)
	}); err != nil {
		return nil, fmt.Errorf("replace document: %w", err)
	}
	return s.currentDocument(entry), nil
}

func (s *LocalStore) readEntries() (map[string]metaEntry, error) {
	data, err := os.ReadFile(s.metaFn)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]metaEntry), nil
		}
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	var entries map[string]metaEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}

	for id, entry := range entries {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.String() != id || entry.ID != id {
			return nil, fmt.Errorf("metadata contains an invalid document ID")
		}
		if entry.BlobExt != "" && !desktopExtension(entry.BlobExt) {
			return nil, fmt.Errorf("metadata contains an invalid document extension")
		}
	}
	if entries == nil {
		entries = make(map[string]metaEntry)
	}

	return entries, nil
}

func (s *LocalStore) AddRef(id, refID string) error {
	unlock, err := LockDirectory(context.Background(), s.base)
	if err != nil {
		return err
	}
	defer unlock()

	entries, err := s.readEntries()
	if err != nil {
		return err
	}

	entry, ok := entries[id]
	if !ok {
		return ErrNotFound
	}

	if _, ok := entries[refID]; !ok {
		return fmt.Errorf("reference target %s not found", refID)
	}

	for _, existing := range entry.Refs {
		if existing == refID {
			return nil
		}
	}

	entry.Refs = append(entry.Refs, refID)
	entries[id] = entry
	return s.saveMeta(entries)
}

func (s *LocalStore) GetRefs(id string) ([]string, error) {
	unlock, err := LockDirectory(context.Background(), s.base)
	if err != nil {
		return nil, err
	}
	defer unlock()

	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}

	entry, ok := entries[id]
	if !ok {
		return nil, ErrNotFound
	}

	if entry.Refs == nil {
		return []string{}, nil
	}
	return entry.Refs, nil
}

// currentDocument deriva tamaño y fecha de la misma fuente que usa Desktop Editors.
func (s *LocalStore) currentDocument(entry metaEntry) *Document {
	doc := entryToDoc(entry)
	if info, err := os.Stat(s.dataPath(entry)); err == nil {
		doc.Size = info.Size()
		if info.ModTime().After(doc.ModifiedAt) {
			doc.ModifiedAt = info.ModTime()
		}
	}
	return doc
}

func entryToDoc(e metaEntry) *Document {
	return &Document{
		ID:         e.ID,
		Filename:   e.Filename,
		Size:       e.Size,
		MimeType:   e.MimeType,
		CreatedAt:  e.CreatedAt,
		ModifiedAt: e.ModifiedAt,
		Refs:       e.Refs,
	}
}

func mimeType(filename string) string {
	ext := filepath.Ext(filename)
	switch ext {
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".pdf":
		return "application/pdf"
	case ".doc":
		return "application/msword"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".txt":
		return "text/plain"
	case ".csv":
		return "text/csv"
	case ".odt":
		return "application/vnd.oasis.opendocument.text"
	case ".ods":
		return "application/vnd.oasis.opendocument.spreadsheet"
	case ".odp":
		return "application/vnd.oasis.opendocument.presentation"
	default:
		return "application/octet-stream"
	}
}
