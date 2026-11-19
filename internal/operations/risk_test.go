package operations

import (
	"strings"
	"testing"
)

func TestRiskStringRoundTrip(t *testing.T) {
	levels := []RiskLevel{
		RiskReadOnly, RiskSafeMutation, RiskSensitiveMutation,
		RiskDestructive, RiskNetwork, RiskAdmin,
	}
	seen := map[string]bool{}
	for i, lvl := range levels {
		if int(lvl) != i {
			t.Fatalf("risk %v out of order: got %d want %d", lvl, int(lvl), i)
		}
		name := lvl.String()
		if name == "unknown" || name == "" {
			t.Fatalf("risk %d has no canonical name", i)
		}
		seen[name] = true
		got, err := ParseRiskLevel(name)
		if err != nil || got != lvl {
			t.Fatalf("round trip %q: got %v, %v", name, got, err)
		}
	}
	if len(seen) != len(levels) {
		t.Fatal("risk names are not unique")
	}
}

func TestParseRiskLevelInvalid(t *testing.T) {
	for _, s := range []string{"", "bogus", "READ_ONLY", " read_only "} {
		if _, err := ParseRiskLevel(s); err == nil {
			t.Fatalf("ParseRiskLevel(%q) expected error", s)
		}
	}
	if (RiskLevel(99).String()) != "unknown" {
		t.Fatal("out-of-range risk should stringify as unknown")
	}
	if (RiskLevel(99)).Valid() {
		t.Fatal("out-of-range risk should be invalid")
	}
	if !strings.Contains(RiskNetwork.String(), "network") {
		t.Fatal("network name unexpected")
	}
}
