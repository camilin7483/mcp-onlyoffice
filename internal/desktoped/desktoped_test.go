package desktoped

import (
	"os"
	"testing"
)

// FindBinary must never launch anything: it only probes well-known paths.
// It returns "" when nothing is installed, or a path that exists.
func TestFindBinarySafe(t *testing.T) {
	got := FindBinary()
	if got == "" {
		return
	}
	fi, err := os.Stat(got)
	if err != nil {
		t.Fatalf("FindBinary returned missing path %q: %v", got, err)
	}
	if fi.IsDir() {
		t.Fatalf("FindBinary returned a directory: %q", got)
	}
}
