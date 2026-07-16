package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionCache stores ephemeral context to reduce redundant data exchange.
// The model only needs to send diffs instead of full state.
type SessionCache struct {
	mu sync.RWMutex

	LastDocID    string    `json:"last_doc_id"`
	LastDocPath  string    `json:"last_doc_path"`
	LastCursor   string    `json:"last_cursor"`
	LastSelText  string    `json:"last_sel_text"`
	LastOp       string    `json:"last_op"`
	LastOpResult string    `json:"last_op_result"`
	LastDir      string    `json:"last_dir"`
	LastTheme    string    `json:"last_theme"`
	LastLang     string    `json:"last_lang"`
	LastTemplate string    `json:"last_template"`
	UpdatedAt    time.Time `json:"updated_at"`

	Prefs Preferences `json:"prefs"`

	persistPath string
}

type Preferences struct {
	Theme       string `json:"theme,omitempty"`
	Language    string `json:"language,omitempty"`
	FontSize    int    `json:"font_size,omitempty"`
	FontFamily  string `json:"font_family,omitempty"`
	AccentColor string `json:"accent_color,omitempty"`
	AutoSave    bool   `json:"auto_save,omitempty"`
}

var defaultCachePath = filepath.Join(os.TempDir(), "mcp-onlyoffice-cache.json")

func NewSessionCache() *SessionCache {
	c := &SessionCache{
		Prefs:       Preferences{Theme: "dark", Language: "es", AutoSave: true},
		persistPath: defaultCachePath,
	}
	c.load()
	return c
}

func (c *SessionCache) SetDoc(id, path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastDocID = id
	c.LastDocPath = path
	c.LastOp = "open_doc"
	c.UpdatedAt = time.Now()
	c.persist()
}

func (c *SessionCache) SetCursor(cursor string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastCursor = cursor
	c.UpdatedAt = time.Now()
}

func (c *SessionCache) SetSelection(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastSelText = text
	c.LastOp = "select"
	c.UpdatedAt = time.Now()
}

func (c *SessionCache) SetOp(op, result string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastOp = op
	c.LastOpResult = result
	c.UpdatedAt = time.Now()
}

func (c *SessionCache) SetDir(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastDir = dir
	c.UpdatedAt = time.Now()
}

func (c *SessionCache) SetPrefs(prefs Preferences) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Prefs = prefs
	c.UpdatedAt = time.Now()
	c.persist()
}

func (c *SessionCache) Get() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := map[string]any{
		"doc_id":    c.LastDocID,
		"doc_path":  c.LastDocPath,
		"last_op":   c.LastOp,
		"cursor":    c.LastCursor,
		"selection": c.LastSelText,
		"dir":       c.LastDir,
		"theme":     c.LastTheme,
		"lang":      c.LastLang,
		"prefs":     c.Prefs,
	}
	return m
}

func (c *SessionCache) GetPrefs() Preferences {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Prefs
}

func (c *SessionCache) persist() {
	b, _ := json.Marshal(c)
	os.WriteFile(c.persistPath, b, 0644)
}

func (c *SessionCache) load() {
	data, err := os.ReadFile(c.persistPath)
	if err != nil {
		return
	}
	var loaded SessionCache
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}
	c.LastDocID = loaded.LastDocID
	c.LastDocPath = loaded.LastDocPath
	c.LastCursor = loaded.LastCursor
	c.LastSelText = loaded.LastSelText
	c.LastOp = loaded.LastOp
	c.LastOpResult = loaded.LastOpResult
	c.LastDir = loaded.LastDir
	c.LastTheme = loaded.LastTheme
	c.LastLang = loaded.LastLang
	c.LastTemplate = loaded.LastTemplate
	c.Prefs = loaded.Prefs
	c.UpdatedAt = loaded.UpdatedAt
}
