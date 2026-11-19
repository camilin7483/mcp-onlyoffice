package operations

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckRevision(t *testing.T) {
	if err := CheckRevision("", ""); err != nil {
		t.Fatalf("both empty must pass: %v", err)
	}
	if err := CheckRevision("abc", "abc"); err != nil {
		t.Fatalf("equal digests must pass: %v", err)
	}
	err := CheckRevision("exp1", "act2")
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("mismatch must yield *ConflictError: %T %v", err, err)
	}
	if ce.Expected != "exp1" || ce.Actual != "act2" {
		t.Fatalf("conflict must carry both digests: %+v", ce)
	}
	if !strings.Contains(ce.Error(), "exp1") || !strings.Contains(ce.Error(), "act2") {
		t.Fatalf("conflict message must carry digests: %v", ce)
	}
	if err := CheckRevision("", "act2"); err == nil {
		t.Fatal("empty vs set must conflict")
	}
}
