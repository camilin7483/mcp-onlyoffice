package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/cam/mcp-onlyoffice/internal/config"
	"github.com/cam/mcp-onlyoffice/internal/desktoped"
	"github.com/cam/mcp-onlyoffice/internal/engine"
	"github.com/cam/mcp-onlyoffice/internal/plugin"
	"github.com/cam/mcp-onlyoffice/internal/storage"
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

type HealthCheck struct {
	Name   string
	Passed bool
	Detail string
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

	bridge := plugin.NewBridge(cfg.PluginPort)

	if err := plugin.InstallPluginFiles(); err != nil {
		slog.Warn("install plugin files", "error", err)
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

	if _, err := os.Stat(filepath.Join(pyEnginePath, "runner.py")); err != nil {
		return nil, fmt.Errorf(
			"python engine runner not found at %s\n\n"+
				"Ensure the python_engine/ directory is next to the mcp-onlyoffice binary.\n"+
				"Expected: %s/runner.py",
			pyEnginePath, pyEnginePath,
		)
	}

	pyBridge := engine.NewBridge(pyEnginePath)
	cache := NewSessionCache()
	registry := NewToolRegistry()
	tools := NewToolHandlers(store, desktopPath, bridge, pyBridge, cache, registry)

	slog.Info("initializing python engine bridge", "path", pyEnginePath)
	if err := pyBridge.Start(ctx); err != nil {
		return nil, fmt.Errorf(
			"python engine initialization failed\n\n"+
				"  Engine path: %s\n"+
				"  Error: %v\n\n"+
				"Possible causes:\n"+
				"  - python3 not available in PATH\n"+
				"  - Missing Python dependencies (run: pip3 install -r %s/requirements.txt)\n"+
				"  - python_engine/runner.py has a syntax error",
			pyEnginePath, err, pyEnginePath,
		)
	}

	caps := pyBridge.Capabilities()
	if len(caps) == 0 {
		runnerPath := filepath.Join(pyEnginePath, "runner.py")
		return nil, fmt.Errorf(
			"python engine started but returned 0 capabilities\n\n"+
				"  Engine path: %s\n"+
				"  Runner: %s\n\n"+
				"Possible causes:\n"+
				"  - Python modules failed to load (check stderr above)\n"+
				"  - Missing dependencies: pip3 install -r %s/requirements.txt\n"+
				"  - __init__.py has import errors",
			pyEnginePath, runnerPath, pyEnginePath,
		)
	}

	slog.Info("python engine bridge ready", "capabilities", len(caps))

	tools.RegisterPythonCapabilities(caps)
	slog.Info("capabilities registered in planner", "total", tools.capabilitiesCount())

	results := runStartupHealthCheck(tools, pyEnginePath, desktopPath)
	allPassed := true
	for _, hc := range results {
		status := "PASS"
		if !hc.Passed {
			status = "FAIL"
			allPassed = false
		}
		slog.Info("health check", "name", hc.Name, "status", status, "detail", hc.Detail)
	}

	if !allPassed {
		var details string
		for _, hc := range results {
			if !hc.Passed {
				details += fmt.Sprintf("\n  [FAIL] %s: %s", hc.Name, hc.Detail)
			}
		}
		return nil, fmt.Errorf("startup health checks failed:%s", details)
	}

	slog.Info("all health checks passed, server ready")
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

func runStartupHealthCheck(tools *ToolHandlers, pyEnginePath, desktopPath string) []HealthCheck {
	var results []HealthCheck

	if tools.pyBridge != nil && tools.pyBridge.Capabilities() != nil && len(tools.pyBridge.Capabilities()) > 0 {
		results = append(results, HealthCheck{
			Name:   "python_engine_connection",
			Passed: true,
			Detail: fmt.Sprintf("%d capabilities loaded from %s", len(tools.pyBridge.Capabilities()), pyEnginePath),
		})
	} else {
		results = append(results, HealthCheck{
			Name:   "python_engine_connection",
			Passed: false,
			Detail: "python engine bridge is nil or has no capabilities",
		})
	}

	if tools.capabilitiesCount() > 0 {
		results = append(results, HealthCheck{
			Name:   "capability_registration",
			Passed: true,
			Detail: fmt.Sprintf("%d capabilities registered in planner discovery", tools.capabilitiesCount()),
		})
	} else {
		results = append(results, HealthCheck{
			Name:   "capability_registration",
			Passed: false,
			Detail: "no capabilities registered in planner discovery — discover will return empty",
		})
	}

	if desktopPath != "" {
		if _, err := os.Stat(desktopPath); err == nil {
			results = append(results, HealthCheck{
				Name:   "onlyoffice_desktop",
				Passed: true,
				Detail: fmt.Sprintf("found at %s", desktopPath),
			})
		} else {
			results = append(results, HealthCheck{
				Name:   "onlyoffice_desktop",
				Passed: false,
				Detail: fmt.Sprintf("not found at %s: %v", desktopPath, err),
			})
		}
	} else {
		results = append(results, HealthCheck{
			Name:   "onlyoffice_desktop",
			Passed: false,
			Detail: "DesktopEditors binary not found — is ONLYOFFICE Desktop Editors installed?",
		})
	}

	results = append(results, HealthCheck{
		Name:   "mcp_server",
		Passed: true,
		Detail: "ready to accept MCP connections",
	})

	return results
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
		Version: "2.0.0",
	}, nil)

	allDefs := s.tools.Definitions()
	for _, t := range allDefs {
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
		"tier1", len(s.registry.Tier1()),
		"tier2", len(s.registry.Tier(2))-len(s.registry.Tier1()),
		"tier3", len(s.registry.Tier(3))-len(s.registry.Tier(2)),
		"total", len(allDefs),
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
		if err == context.Canceled {
			<-done
			return nil
		}
		return err
	}

	<-done
	return nil
}

func (s *MCPOnlyOffice) addToolToServer(server *mcp.Server, t toolDef) {
	server.AddTool(&mcp.Tool{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: t.InputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
