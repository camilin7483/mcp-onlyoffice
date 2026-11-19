package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("MCP_ONLYOFFICE_PLUGIN_PORT", "9999")
	t.Setenv("MCP_ONLYOFFICE_STORAGE_PATH", "/tmp/test-store")
	t.Setenv("MCP_ONLYOFFICE_LOG_LEVEL", "debug")
	t.Setenv("MCP_ONLYOFFICE_ALLOWED_ROOTS", "/tmp/a:/tmp/b")

	cfg := Default()
	cfg.ApplyEnv()

	if cfg.PluginPort != 9999 {
		t.Fatalf("PluginPort = %d, want 9999", cfg.PluginPort)
	}
	if cfg.StoragePath != "/tmp/test-store" {
		t.Fatalf("StoragePath = %q", cfg.StoragePath)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("LogLevel = %q", cfg.LogLevel)
	}
	if len(cfg.AllowedRoots) != 2 || cfg.AllowedRoots[0] != "/tmp/a" || cfg.AllowedRoots[1] != "/tmp/b" {
		t.Fatalf("AllowedRoots = %v", cfg.AllowedRoots)
	}
}

func TestLoadWithConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"plugin_port": 4321, "storage_path": "/tmp/from-file", "log_level": "warn"}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PluginPort != 4321 {
		t.Fatalf("PluginPort = %d, want 4321", cfg.PluginPort)
	}
	if cfg.StoragePath != "/tmp/from-file" {
		t.Fatalf("StoragePath = %q", cfg.StoragePath)
	}
	if cfg.LogLevel != "warn" {
		t.Fatalf("LogLevel = %q", cfg.LogLevel)
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PluginPort != 2021 {
		t.Fatalf("PluginPort = %d, want default 2021", cfg.PluginPort)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestEffectiveAllowedRoots(t *testing.T) {
	def := &Config{StoragePath: "/tmp/store"}
	roots := def.EffectiveAllowedRoots()
	if len(roots) != 1 || roots[0] != "/tmp/store" {
		t.Fatalf("default roots = %v, want [StoragePath]", roots)
	}

	custom := &Config{StoragePath: "/tmp/store", AllowedRoots: []string{"", "   ", "/", "/tmp/a"}}
	roots = custom.EffectiveAllowedRoots()
	if len(roots) != 1 || roots[0] != "/tmp/a" {
		t.Fatalf("filtered roots = %v, want [/tmp/a]", roots)
	}
}

func TestBuilderPathEnvAndFile(t *testing.T) {
	t.Setenv("MCP_ONLYOFFICE_BUILDER_PATH", "/tmp/fake-docbuilder")
	cfg := Default()
	cfg.ApplyEnv()
	if cfg.BuilderPath != "/tmp/fake-docbuilder" {
		t.Fatalf("BuilderPath = %q, want the env value", cfg.BuilderPath)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"builder_path": "/opt/db"}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("MCP_ONLYOFFICE_BUILDER_PATH", "")
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.BuilderPath != "/opt/db" {
		t.Fatalf("BuilderPath = %q, want /opt/db", loaded.BuilderPath)
	}
}
