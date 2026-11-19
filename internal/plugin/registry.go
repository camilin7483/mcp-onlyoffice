package plugin

import (
	"sort"
	"sync"
	"time"
)

// Capability required by each of the six editor methods. Methods absent
// from the map (e.g. ping) need no capability and pass through.
//
//	get_selected_text -> reader.selected_text
//	get_all_content   -> reader.full_content   (Office API inside callCommand: word only)
//	insert_text       -> writer.insert_text
//	replace_text      -> writer.replace_text   (needs full-content read + SearchAndReplace)
//	create_table      -> writer.create_table
//	create_list       -> writer.create_list
var methodCapabilities = map[string]string{
	"get_selected_text": "reader.selected_text",
	"get_all_content":   "reader.full_content",
	"insert_text":       "writer.insert_text",
	"replace_text":      "writer.replace_text",
	"create_table":      "writer.create_table",
	"create_list":       "writer.create_list",
}

// mutationMethods bump Session.Revision after an ok response.
var mutationMethods = map[string]bool{
	"insert_text":  true,
	"replace_text": true,
	"create_table": true,
	"create_list":  true,
}

// MaxSessions caps concurrent plugin connections. Each editor window loads
// its own plugin instance, so several coexist.
const MaxSessions = 8

// MaxPendingPerSession caps in-flight commands per session.
const MaxPendingPerSession = 32

// Session is one handshake-verified plugin connection. DocumentID stays
// empty until bound (ONLYOFFICE exposes no document id to plugins) and
// Revision is a server-side counter bumped on each accepted mutation.
type Session struct {
	ID               string
	PluginInstanceID string
	EditorType       string
	Capabilities     map[string]bool
	DocumentID       string
	Revision         uint64
	ConnectedAt      time.Time
	LastSeen         time.Time
}

// SessionInfo is the read-only snapshot of a Session: ids, editor,
// capabilities and counters only. No addresses, no tokens.
type SessionInfo struct {
	ID               string    `json:"id"`
	PluginInstanceID string    `json:"plugin_instance_id"`
	EditorType       string    `json:"editor_type"`
	Capabilities     []string  `json:"capabilities"`
	DocumentID       string    `json:"document_id"`
	Revision         uint64    `json:"revision"`
	ConnectedAt      time.Time `json:"connected_at"`
	Active           bool      `json:"active"`
}

// Registry holds up to MaxSessions sessions plus the active one: the most
// recently connected or used session, target of legacy SendCommand.
type Registry struct {
	mu       sync.Mutex
	sessions map[string]*sessionEntry
	activeID string
}

func NewRegistry() *Registry {
	return &Registry{sessions: make(map[string]*sessionEntry)}
}

func (r *Registry) add(e *sessionEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.sessions[e.ID]; dup {
		return errPluginError("duplicate session id")
	}
	if len(r.sessions) >= MaxSessions {
		return errPluginError("session limit reached (8)")
	}
	r.sessions[e.ID] = e
	r.activeID = e.ID
	return nil
}

func (r *Registry) remove(id string) *sessionEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.sessions[id]
	if e == nil {
		return nil
	}
	delete(r.sessions, id)
	if r.activeID == id {
		r.activeID = ""
		for sid, se := range r.sessions {
			if r.activeID == "" || se.ConnectedAt.After(r.sessions[r.activeID].ConnectedAt) {
				r.activeID = sid
			}
		}
	}
	return e
}

func (r *Registry) get(id string) *sessionEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[id]
}

func (r *Registry) active() *sessionEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeID == "" {
		return nil
	}
	return r.sessions[r.activeID]
}

func (r *Registry) touch(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[id]; ok {
		r.activeID = id
	}
}

// Count reports live sessions.
func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

// Sessions returns the read-only snapshot with capabilities sorted.
func (r *Registry) Sessions() []SessionInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SessionInfo, 0, len(r.sessions))
	for id, e := range r.sessions {
		caps := make([]string, 0, len(e.Capabilities))
		for c := range e.Capabilities {
			caps = append(caps, c)
		}
		sort.Strings(caps)
		out = append(out, SessionInfo{
			ID:               e.ID,
			PluginInstanceID: e.PluginInstanceID,
			EditorType:       e.EditorType,
			Capabilities:     caps,
			DocumentID:       e.DocumentID,
			Revision:         e.Revision,
			ConnectedAt:      e.ConnectedAt,
			Active:           id == r.activeID,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ConnectedAt.Equal(out[j].ConnectedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].ConnectedAt.Before(out[j].ConnectedAt)
	})
	return out
}
