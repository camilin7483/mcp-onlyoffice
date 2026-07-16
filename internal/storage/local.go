package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type LocalStore struct {
	mu     sync.RWMutex
	base   string
	metaFn string
}

func NewLocalStore(base string) (*LocalStore, error) {
	abs, err := filepath.Abs(base)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	if err := os.MkdirAll(abs, 0755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
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
}

func (s *LocalStore) metaPath(id string) string {
	return filepath.Join(s.base, id+".meta.json")
}

func (s *LocalStore) dataPath(id string) string {
	return filepath.Join(s.base, id+".data")
}

func (s *LocalStore) saveMeta(entries map[string]metaEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := os.WriteFile(s.metaFn, data, 0644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}

func (s *LocalStore) Create(filename string, data []byte) (*Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	if err := os.WriteFile(s.dataPath(id), data, 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	entries[id] = entry
	if err := s.saveMeta(entries); err != nil {
		return nil, err
	}

	return entryToDoc(entry), nil
}

func (s *LocalStore) Get(id string) (*Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}

	entry, ok := entries[id]
	if !ok {
		return nil, ErrNotFound
	}

	return entryToDoc(entry), nil
}

func (s *LocalStore) GetData(id string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}

	if _, ok := entries[id]; !ok {
		return nil, ErrNotFound
	}

	data, err := os.ReadFile(s.dataPath(id))
	if err != nil {
		return nil, fmt.Errorf("read file data: %w", err)
	}

	return data, nil
}

func (s *LocalStore) List() ([]Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}

	docs := make([]Document, 0, len(entries))
	for _, e := range entries {
		docs = append(docs, *entryToDoc(e))
	}

	sort.Slice(docs, func(i, j int) bool {
		return docs[i].CreatedAt.After(docs[j].CreatedAt)
	})

	return docs, nil
}

func (s *LocalStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readEntries()
	if err != nil {
		return err
	}

	if _, ok := entries[id]; !ok {
		return ErrNotFound
	}

	if err := os.Remove(s.dataPath(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove data file: %w", err)
	}
	if err := os.Remove(s.metaPath(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove meta file: %w", err)
	}
	delete(entries, id)

	return s.saveMeta(entries)
}

func (s *LocalStore) DataPath(id string) string {
	return s.dataPath(id)
}

func (s *LocalStore) Update(id string, data []byte) (*Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readEntries()
	if err != nil {
		return nil, err
	}

	entry, ok := entries[id]
	if !ok {
		return nil, ErrNotFound
	}

	entry.Size = int64(len(data))
	entry.ModifiedAt = time.Now()

	if err := os.WriteFile(s.dataPath(id), data, 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	entries[id] = entry
	if err := s.saveMeta(entries); err != nil {
		return nil, err
	}

	return entryToDoc(entry), nil
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

	if entries == nil {
		entries = make(map[string]metaEntry)
	}

	return entries, nil
}

func (s *LocalStore) AddRef(id, refID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	s.mu.RLock()
	defer s.mu.RUnlock()

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
