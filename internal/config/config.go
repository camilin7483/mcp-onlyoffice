package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	PluginPort  int    `json:"plugin_port"`
	StoragePath string `json:"storage_path"`
	DesktopPath string `json:"desktop_path"`
	LogLevel    string `json:"log_level"`
}

func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		PluginPort:  2021,
		StoragePath: filepath.Join(home, "mcp-onlyoffice", "documents"),
		DesktopPath: "",
		LogLevel:    "info",
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
