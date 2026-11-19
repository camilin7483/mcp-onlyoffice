// Package operations is the typed model for document mutations: risk
// levels, operation schema, registry and approval policy, revision
// conflicts, and transactions with receipts. It depends only on the
// standard library so higher layers can adopt it without cycles.
package operations

import "fmt"

// RiskLevel orders operation risk from least to most privileged.
type RiskLevel int

const (
	RiskReadOnly RiskLevel = iota
	RiskSafeMutation
	RiskSensitiveMutation
	RiskDestructive
	RiskNetwork
	RiskAdmin
)

// String returns the canonical risk name.
func (r RiskLevel) String() string {
	switch r {
	case RiskReadOnly:
		return "read_only"
	case RiskSafeMutation:
		return "safe_mutation"
	case RiskSensitiveMutation:
		return "sensitive_mutation"
	case RiskDestructive:
		return "destructive"
	case RiskNetwork:
		return "network"
	case RiskAdmin:
		return "admin"
	default:
		return "unknown"
	}
}

// Valid reports whether r is a defined risk level.
func (r RiskLevel) Valid() bool {
	return r >= RiskReadOnly && r <= RiskAdmin
}

// ParseRiskLevel parses a canonical risk name (exact match).
func ParseRiskLevel(s string) (RiskLevel, error) {
	switch s {
	case "read_only":
		return RiskReadOnly, nil
	case "safe_mutation":
		return RiskSafeMutation, nil
	case "sensitive_mutation":
		return RiskSensitiveMutation, nil
	case "destructive":
		return RiskDestructive, nil
	case "network":
		return RiskNetwork, nil
	case "admin":
		return RiskAdmin, nil
	default:
		return RiskReadOnly, fmt.Errorf("operations: unknown risk level %q", s)
	}
}
