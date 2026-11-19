package operations

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is the known set of operations.
type Registry struct {
	mu    sync.RWMutex
	specs map[string]Spec
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{specs: make(map[string]Spec)}
}

// Register adds a spec; duplicate names and empty/invalid fields are errors.
func (r *Registry) Register(s Spec) error {
	if s.Name == "" {
		return fmt.Errorf("operations: register requires a non-empty name")
	}
	if !s.Backend.Valid() {
		return fmt.Errorf("operations: register %q requires a known backend", s.Name)
	}
	if !s.Risk.Valid() {
		return fmt.Errorf("operations: register %q requires a known risk level", s.Name)
	}
	if s.Capability == "" {
		return fmt.Errorf("operations: register %q requires a non-empty capability", s.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.specs[s.Name]; dup {
		return fmt.Errorf("operations: duplicate operation %q", s.Name)
	}
	r.specs[s.Name] = s
	return nil
}

// Lookup returns the spec for name.
func (r *Registry) Lookup(name string) (Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.specs[name]
	return s, ok
}

// Names returns registered operation names, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.specs))
	for n := range r.specs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Decision is the approval outcome for one operation.
type Decision struct {
	Allow           bool
	RequireApproval bool
	RequireDiff     bool
	Reason          string
}

// Policy gates operations; Approved holds operation names pre-approved
// for this process (waives human approval for sensitive mutations only).
type Policy struct {
	Approved map[string]bool
}

// Decide applies the fixed risk rules to spec.
func (p *Policy) Decide(spec Spec) Decision {
	switch spec.Risk {
	case RiskReadOnly:
		return Decision{Allow: true, Reason: "read-only operation allowed"}
	case RiskSafeMutation:
		return Decision{Allow: true, Reason: "safe mutation allowed"}
	case RiskSensitiveMutation:
		if p != nil && p.Approved[spec.Name] {
			return Decision{Allow: true, RequireDiff: true, Reason: "sensitive mutation approved for this process; diff required"}
		}
		return Decision{Allow: true, RequireApproval: true, RequireDiff: true, Reason: "sensitive mutation requires approval and diff"}
	case RiskDestructive:
		return Decision{Allow: true, RequireApproval: true, RequireDiff: true, Reason: "destructive operation requires approval and diff even when pre-approved"}
	case RiskNetwork, RiskAdmin:
		return Decision{Allow: false, Reason: spec.Risk.String() + " operations are disabled by default"}
	default:
		return Decision{Allow: false, Reason: "unknown risk level: denied"}
	}
}
