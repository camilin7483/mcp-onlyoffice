package storage

import (
	"errors"
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
