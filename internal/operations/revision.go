package operations

import "fmt"

// ConflictError reports an expected-revision mismatch.
type ConflictError struct {
	DocumentID       string
	Expected, Actual string
}

// Error names the conflict and carries both digests so callers can retry.
func (e *ConflictError) Error() string {
	if e == nil {
		return "operations: revision conflict"
	}
	return fmt.Sprintf("operations: revision conflict for document %q: expected %q, actual %q",
		e.DocumentID, e.Expected, e.Actual)
}

// CheckRevision compares opaque digests; both empty means no check.
func CheckRevision(expected, actual string) error {
	if expected == "" && actual == "" {
		return nil
	}
	if expected == actual {
		return nil
	}
	return &ConflictError{Expected: expected, Actual: actual}
}
