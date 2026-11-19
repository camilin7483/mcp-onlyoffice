package operations

// Backend identifies the execution backend for an operation.
type Backend string

const (
	BackendDesktop         Backend = "desktop"
	BackendOOXML           Backend = "ooxml"
	BackendDocumentBuilder Backend = "document_builder"
	BackendPython          Backend = "python"
)

// Valid reports whether b is a known backend.
func (b Backend) Valid() bool {
	switch b {
	case BackendDesktop, BackendOOXML, BackendDocumentBuilder, BackendPython:
		return true
	default:
		return false
	}
}

// Target locates what an operation acts on.
type Target struct {
	DocumentID string `json:"document_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Path       string `json:"path,omitempty"`      // for filesystem/OOXML backends
	Paragraph  *int   `json:"paragraph,omitempty"` // 0-based, nil = whole document
	RangeStart *int   `json:"range_start,omitempty"`
	RangeEnd   *int   `json:"range_end,omitempty"`
}

// Operation is a single requested document mutation.
type Operation struct {
	Name             string         `json:"operation"` // e.g. "writer.replace_range"
	Target           Target         `json:"target"`
	Params           map[string]any `json:"params,omitempty"`
	ExpectedRevision string         `json:"expected_revision,omitempty"` // opaque digest
	DryRun           bool           `json:"dry_run,omitempty"`
}

// Change describes one observable before/after effect.
type Change struct {
	Kind   string `json:"kind"` // "replace", "insert", "delete", "format", "create"
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	Where  string `json:"where,omitempty"` // human-usable location, no absolute paths
}

// Diff is the reviewable effect of an operation (or dry run).
type Diff struct {
	Changes []Change `json:"changes"`
	Summary string   `json:"summary"`
}

// Spec registers one known operation with its risk and requirements.
type Spec struct {
	Name           string
	Backend        Backend
	Risk           RiskLevel
	Capability     string // plugin capability or backend feature required
	Reversible     bool
	SupportsDryRun bool
}
