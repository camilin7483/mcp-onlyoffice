package main

import (
	"testing"

	"github.com/cam/mcp-onlyoffice/internal/config"
)

func TestApplyPluginDirFlagWins(t *testing.T) {
	cfg := config.Default()
	cfg.PluginDir = "/from-config"
	applyPluginDirFlag(cfg, "/from-flag")
	if cfg.PluginDir != "/from-flag" {
		t.Fatalf("PluginDir = %q, want the flag value", cfg.PluginDir)
	}
}

func TestApplyPluginDirFlagEmptyKeepsConfig(t *testing.T) {
	cfg := config.Default()
	cfg.PluginDir = "/from-config"
	applyPluginDirFlag(cfg, "")
	if cfg.PluginDir != "/from-config" {
		t.Fatalf("PluginDir = %q, want the config value", cfg.PluginDir)
	}
}
