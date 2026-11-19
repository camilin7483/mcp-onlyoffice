package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	auditlog "github.com/cam/mcp-onlyoffice/internal/audit"
	"github.com/cam/mcp-onlyoffice/internal/builder"
	"github.com/cam/mcp-onlyoffice/internal/config"
	"github.com/cam/mcp-onlyoffice/internal/desktoped"
	"github.com/cam/mcp-onlyoffice/internal/engine"
	"github.com/cam/mcp-onlyoffice/internal/plugin"
	"github.com/cam/mcp-onlyoffice/internal/storage"
	"github.com/cam/mcp-onlyoffice/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPOnlyOffice struct {
	cfg      *config.Config
	store    *storage.LocalStore
	bridge   *plugin.Bridge
	pyBridge *engine.Bridge
	tools    *ToolHandlers
	cache    *SessionCache
	registry *ToolRegistry
	ctx      context.Context
}

func New(ctx context.Context, cfg *config.Config) (*MCPOnlyOffice, error) {
	if err := os.MkdirAll(cfg.StoragePath, 0755); err != nil {
		return nil, fmt.Errorf("create storage dir %s: %w", cfg.StoragePath, err)
	}

	store, err := storage.NewLocalStore(cfg.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
	}

	desktopPath := desktoped.FindBinary()
	if cfg.DesktopPath != "" {
		desktopPath = cfg.DesktopPath
	}
	desktopAvailable := false
	if desktopPath != "" {
		if _, err := os.Stat(desktopPath); err == nil {
			desktopAvailable = true
		} else {
			slog.Warn("onlyoffice desktop not found, continuing without desktop integration", "path", desktopPath, "error", err)
		}
	} else {
		slog.Warn("onlyoffice desktop not found, continuing without desktop integration")
	}

	bridge := plugin.NewBridge(cfg.PluginPort)

	// The plugin install never fails startup: resolve the configured dir
	// (flag > env/config > verified Linux default) and keep serving when
	// it cannot be installed. On non-Linux without -plugin-dir the
	// resolver only warns, per plugin.ResolvePluginDir.
	if dir, err := plugin.ResolvePluginDir(cfg.PluginDir); err != nil {
		slog.Warn("plugin dir unresolved, continuing without plugin install", "error", err)
	} else if err := plugin.InstallPluginFilesTo(dir); err != nil {
		slog.Warn("plugin install failed, continuing without plugin", "dir", dir, "error", err)
	} else {
		slog.Info("plugin installed", "dir", dir)
	}

	exeDir := filepath.Dir(os.Args[0])
	pyEnginePath := filepath.Join(exeDir, "python_engine")
	if _, err := os.Stat(filepath.Join(pyEnginePath, "runner.py")); err != nil {
		pyEnginePath = filepath.Join(filepath.Dir(exeDir), "python_engine")
		if _, err := os.Stat(filepath.Join(pyEnginePath, "runner.py")); err != nil {
			cwd, _ := os.Getwd()
			pyEnginePath = filepath.Join(cwd, "python_engine")
		}
	}

	pyBridge := engine.NewBridge(pyEnginePath)
	pyBridge.OutputDir = cfg.StoragePath
	cache := NewSessionCache()
	registry := NewToolRegistry()
	tools := NewToolHandlers(store, desktopPath, bridge, pyBridge, cache, registry)
	tools.SetAllowedRoots(cfg.EffectiveAllowedRoots()...)
	tools.SetAuditLogger(auditlog.NewSilent(cfg.StoragePath))
	// Optional Document Builder backend: detection never fails and an absent
	// binary only disables builder tools, never the core.
	bld := builder.Detect(cfg.BuilderPath)
	bld.SetRoots(cfg.EffectiveAllowedRoots())
	tools.SetBuilder(bld)
	if bld.Available() {
		slog.Info("document builder backend ready", "path", bld.Path())
	} else {
		slog.Warn("document builder not found, continuing without builder backend")
	}
	tools.toolset = cfg.EffectiveToolset()
	tools.desktopAvailable = desktopAvailable

	pythonAvailable := false
	slog.Info("initializing python engine bridge", "path", pyEnginePath)
	if _, err := os.Stat(filepath.Join(pyEnginePath, "runner.py")); err != nil {
		slog.Warn("python engine unavailable, continuing with reduced toolset", "path", pyEnginePath, "error", "runner.py not found")
	} else if err := pyBridge.Start(ctx); err != nil {
		slog.Warn("python engine unavailable, continuing with reduced toolset", "path", pyEnginePath, "error", err)
	} else if caps := pyBridge.Capabilities(); len(caps) == 0 {
		slog.Warn("python engine unavailable, continuing with reduced toolset", "path", pyEnginePath, "error", "engine started but returned 0 capabilities")
	} else {
		pythonAvailable = true
		tools.RegisterPythonCapabilities(pyBridge.Capabilities())
		slog.Info("python engine bridge ready", "capabilities", len(pyBridge.Capabilities()))
	}
	tools.pythonAvailable = pythonAvailable

	health := tools.Health()
	for _, flag := range []struct {
		name  string
		value any
	}{
		{"core_ready", health.CoreReady},
		{"python_available", health.PythonAvailable},
		{"desktop_available", health.DesktopAvailable},
		{"builder_available", health.BuilderAvailable},
		{"document_server_available", health.DocumentServerAvailable},
		{"storage_writable", health.StorageWritable},
		{"tools_registered", health.ToolsRegistered},
		{"python_capabilities", health.PythonCapabilities},
		{"allowed_roots", health.AllowedRoots},
	} {
		slog.Info("startup readiness", "flag", flag.name, "value", flag.value)
	}

	slog.Info("core ready, serving with degraded backends where unavailable")
	return &MCPOnlyOffice{
		cfg:      cfg,
		store:    store,
		bridge:   bridge,
		pyBridge: pyBridge,
		tools:    tools,
		cache:    cache,
		registry: registry,
		ctx:      ctx,
	}, nil
}

func (s *MCPOnlyOffice) Run() error {
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	go func() {
		if err := s.bridge.Start(); err != nil {
			slog.Warn("plugin bridge", "error", err)
		}
	}()

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "mcp-onlyoffice",
		Version: version.Version,
	}, nil)

	allDefs := s.tools.Definitions()
	activeDefs := allDefs
	toolset := s.cfg.EffectiveToolset()
	if toolset == config.ToolsetProfessional {
		activeDefs = FilterProfessionalTools(allDefs)
	}
	for _, t := range activeDefs {
		tier := 3
		if isEssential(t.Name) {
			tier = 1
		} else if isCommon(t.Name) {
			tier = 2
		}
		s.registry.Register(t, tier)
		s.addToolToServer(server, t)
	}

	slog.Info("tools registered",
		"toolset", toolset,
		"tier1", len(s.registry.Tier1()),
		"tier2", len(s.registry.Tier(2))-len(s.registry.Tier1()),
		"tier3", len(s.registry.Tier(3))-len(s.registry.Tier(2)),
		"total", len(activeDefs),
	)

	done := make(chan struct{}, 1)
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		select {
		case <-sigCh:
			slog.Info("shutting down...")
			cancel()
		case <-ctx.Done():
		}
		close(done)
	}()

	slog.Info("starting mcp-onlyoffice", "pid", os.Getpid())
	slog.Info("storage path", "path", s.cfg.StoragePath)
	slog.Info("bridge address", "addr", s.bridge.ListenAddr())

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		cancel()
		<-done
		if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	cancel()
	<-done
	return nil
}

func (s *MCPOnlyOffice) addToolToServer(server *mcp.Server, t toolDef) {
	server.AddTool(MCPToolFromDef(t), func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: "invalid arguments: " + err.Error()}},
					IsError: true,
				}, nil
			}
		} else {
			args = make(map[string]any)
		}
		return t.Handler(ctx, args)
	})
}

func isEssential(name string) bool {
	switch name {
	case "read_file", "write_text", "append_text", "list_directory",
		"create_directory", "copy_file", "move_file", "delete_file",
		"file_info", "find_files", "file_exists",
		"create_document", "get_document", "list_documents",
		"delete_document", "open_document",
		"insert_text", "replace_text", "get_all_content", "get_selected_text",
		"discover_tools", "python_engine":
		return true
	}
	return false
}

func isCommon(name string) bool {
	switch name {
	case "write_document", "write_docx", "write_table_docx",
		"write_spreadsheet", "write_presentation",
		"read_csv", "write_csv", "csv_to_json", "json_to_csv",
		"read_json", "write_json",
		"import_document", "export_document",
		"checksum_file", "head_file", "tail_file", "word_count",
		"disk_usage", "tree_directory",
		"create_word_document", "create_spreadsheet", "create_presentation",
		"create_table", "create_list", "touch_file",
		"create_zip", "extract_zip",
		"write_markdown",
		"document_agent", "planner", "list_capabilities":
		return true
	}
	return false
}
