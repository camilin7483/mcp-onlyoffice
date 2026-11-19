package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/cam/mcp-onlyoffice/internal/config"
	"github.com/cam/mcp-onlyoffice/internal/server"
)

func main() {
	configPath := flag.String("config", "", "path to config.json")
	showHelp := flag.Bool("help", false, "show usage")
	flag.Parse()

	if *showHelp {
		fmt.Println(`mcp-onlyoffice — MCP server for ONLYOFFICE Desktop Editors

Usage:
  mcp-onlyoffice [flags]

Flags:
  -config path   Path to config.json (optional, uses defaults + env vars)
  -help          Show this help

Environment variables:
  MCP_ONLYOFFICE_PLUGIN_PORT    Plugin WebSocket port (default: 2021)
  MCP_ONLYOFFICE_STORAGE_PATH   Document storage directory
  MCP_ONLYOFFICE_DESKTOP_PATH   Path to DesktopEditors binary

The server communicates with the AI agent via MCP stdio protocol,
and with the ONLYOFFICE plugin via WebSocket on localhost.

Example config.json:
{
  "plugin_port": 2021,
  "storage_path": "/home/user/mcp-onlyoffice/documents",
  "desktop_path": "/opt/onlyoffice/desktopeditors/DesktopEditors",
  "log_level": "info"
}`)
		return
	}

	cfg := config.Default()
	cfg.ApplyEnv()

	if *configPath != "" {
		var err error
		cfg, err = config.Load(*configPath)
		if err != nil {
			slog.Error("config error", "error", err)
			os.Exit(1)
		}
	}

	logLevel := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})))

	ctx := context.Background()

	app, err := server.New(ctx, cfg)
	if err != nil {
		slog.Error("init error", "error", err)
		os.Exit(1)
	}

	if err := app.Run(); err != nil {
		slog.Error("run error", "error", err)
		os.Exit(1)
	}
}
