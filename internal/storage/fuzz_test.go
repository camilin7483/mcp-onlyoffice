package storage

// FuzzResolvePath feeds arbitrary path strings through the sandbox:
// it must never panic, and an accepted path is always inside the roots.

import (
	"path/filepath"
	"strings"
	"testing"
)

func FuzzResolvePath(f *testing.F) {
	f.Add("/tmp/mcp-onlyoffice/file.txt")
	f.Add("/tmp/mcp-onlyoffice/sub/../file.txt")
	f.Add("/etc/passwd")
	f.Add("/tmp/mcp-onlyoffice/../../etc/passwd")
	f.Add("\x00")
	f.Add("/tmp/mcp-onlyoffice/a\x00b")
	f.Add("")
	f.Add("/")
	f.Add("relative/path.txt")

	f.Fuzz(func(t *testing.T, p string) {
		if len(p) > 4096 {
			t.Skip("input over cheap-fuzz budget")
		}
		roots := []string{"/tmp/mcp-onlyoffice-fuzz-root"}
		got, err := ResolvePath(p, roots)
		if err != nil {
			return
		}
		root := filepath.Clean(roots[0])
		if got != root && !strings.HasPrefix(got, root+"/") {
			t.Fatalf("ResolvePath(%q) = %q, outside roots %q", p, got, roots)
		}
	})
}
