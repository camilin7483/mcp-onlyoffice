package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePluginDirOverrideWins(t *testing.T) {
	dir, err := ResolvePluginDir("/tmp/custom-plugins")
	if err != nil {
		t.Fatalf("ResolvePluginDir: %v", err)
	}
	if dir != "/tmp/custom-plugins" {
		t.Fatalf("dir = %q, want the override", dir)
	}
}

func TestResolvePluginDirDefault(t *testing.T) {
	dir, err := ResolvePluginDir("")
	if err != nil {
		t.Fatalf("ResolvePluginDir: %v", err)
	}
	if !strings.HasSuffix(dir, pluginFolder) {
		t.Fatalf("default dir = %q, want it to end in %q", dir, pluginFolder)
	}
}

func TestInstallPluginFilesToTempDir(t *testing.T) {
	dir := t.TempDir()
	if err := InstallPluginFilesTo(dir); err != nil {
		t.Fatalf("InstallPluginFilesTo: %v", err)
	}
	for _, name := range []string{"config.json", "index.html"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestInstallPluginFilesToUncreatableDir(t *testing.T) {
	// A path below a regular file can never be created: MkdirAll fails
	// and the error must point at the -plugin-dir override.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	err := InstallPluginFilesTo(filepath.Join(blocker, "sub"))
	if err == nil {
		t.Fatal("expected an error for an uncreatable dir")
	}
	if !strings.Contains(err.Error(), "-plugin-dir") {
		t.Fatalf("error should name -plugin-dir, got: %v", err)
	}
}

func TestInstallPluginFilesToEmptyDir(t *testing.T) {
	if err := InstallPluginFilesTo(""); err == nil {
		t.Fatal("expected an error for an empty dir")
	} else if !strings.Contains(err.Error(), "-plugin-dir") {
		t.Fatalf("error should name -plugin-dir, got: %v", err)
	}
}
