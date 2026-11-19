package version

import (
	"regexp"
	"testing"
)

func TestVersions(t *testing.T) {
	semver := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	cases := []struct {
		name  string
		value string
	}{
		{"Version", Version},
		{"EngineProtocol", EngineProtocol},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !semver.MatchString(tc.value) {
				t.Fatalf("%s = %q, want semver shape X.Y.Z", tc.name, tc.value)
			}
		})
	}
	if BridgeProtocol < 1 {
		t.Fatalf("BridgeProtocol = %d, want >= 1", BridgeProtocol)
	}
}
