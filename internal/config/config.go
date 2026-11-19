package config

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Perfiles de herramientas disponibles.
const (
	ToolsetLegacy       = "legacy"
	ToolsetProfessional = "professional"
	ToolsetEnvVar       = "MCP_ONLYOFFICE_TOOLSET"
)

type Config struct {
	PluginPort   int      `json:"plugin_port"`
	PluginDir    string   `json:"plugin_dir"`
	StoragePath  string   `json:"storage_path"`
	DesktopPath  string   `json:"desktop_path"`
	BuilderPath  string   `json:"builder_path"`
	LogLevel     string   `json:"log_level"`
	AllowedRoots []string `json:"allowed_roots"`
	Toolset      string   `json:"toolset"`
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
		Toolset:      ToolsetLegacy,
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
	if v := os.Getenv("MCP_ONLYOFFICE_BUILDER_PATH"); v != "" {
		c.BuilderPath = v
	}
	if v := os.Getenv("MCP_ONLYOFFICE_PLUGIN_DIR"); v != "" {
		c.PluginDir = v
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
	if v := os.Getenv(ToolsetEnvVar); v != "" {
		c.Toolset = normalizeToolset(v)
	}
}

// EffectiveToolset devuelve el perfil activo, con "legacy" como valor seguro.
func (c *Config) EffectiveToolset() string {
	return normalizeToolset(c.Toolset)
}

// normalizeToolset acepta solo los perfiles conocidos; lo demás cae a legacy.
func normalizeToolset(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case ToolsetProfessional:
		return ToolsetProfessional
	case "", ToolsetLegacy:
		return ToolsetLegacy
	default:
		slog.Warn("unknown toolset, falling back to legacy", "value", v)
		return ToolsetLegacy
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
