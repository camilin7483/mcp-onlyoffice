package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	PluginPort   int      `json:"plugin_port"`
	StoragePath  string   `json:"storage_path"`
	DesktopPath  string   `json:"desktop_path"`
	LogLevel     string   `json:"log_level"`
	AllowedRoots []string `json:"allowed_roots"`
}

func Default() *Config {
	home, _ := os.UserHomeDir()
	storage := filepath.Join(home, "mcp-onlyoffice", "documents")
	return &Config{
		PluginPort:   2021,
		StoragePath:  storage,
		DesktopPath:  "",
		LogLevel:     "info",
		AllowedRoots: nil, // computed from the final StoragePath in EffectiveAllowedRoots
	}
}

func (c *Config) ApplyEnv() {
	if v := os.Getenv("MCP_ONLYOFFICE_PLUGIN_PORT"); v != "" {
		if p, e := strconv.Atoi(v); e == nil {
			c.PluginPort = p
		}
	}
	if v := os.Getenv("MCP_ONLYOFFICE_STORAGE_PATH"); v != "" {
		c.StoragePath = v
	}
	if v := os.Getenv("MCP_ONLYOFFICE_DESKTOP_PATH"); v != "" {
		c.DesktopPath = v
	}
	if v := os.Getenv("MCP_ONLYOFFICE_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := os.Getenv("MCP_ONLYOFFICE_ALLOWED_ROOTS"); v != "" {
		var roots []string
		for _, r := range strings.Split(v, ":") {
			if r = strings.TrimSpace(r); r != "" {
				roots = append(roots, r)
			}
		}
		if len(roots) > 0 {
			c.AllowedRoots = roots
		}
	}
}

// EffectiveAllowedRoots returns the configured roots, defaulting to the
// storage directory plus the current working directory.
func (c *Config) EffectiveAllowedRoots() []string {
	roots := c.AllowedRoots
	if len(roots) == 0 {
		roots = []string{c.StoragePath}
	}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" || filepath.Clean(r) == string(os.PathSeparator) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg.ApplyEnv()
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	cfg.ApplyEnv()

	return cfg, nil
}
