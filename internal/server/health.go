package server

import (
	"context"
	"encoding/json"
	"os"

	"github.com/cam/mcp-onlyoffice/internal/config"
	"github.com/cam/mcp-onlyoffice/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Health is the MCP-visible readiness snapshot of the core and its
// optional backends. Counts only, never paths or secrets.
type Health struct {
	CoreReady               bool   `json:"core_ready"`
	PythonAvailable         bool   `json:"python_available"`
	DesktopAvailable        bool   `json:"desktop_available"`
	BuilderAvailable        bool   `json:"builder_available"`
	DocumentServerAvailable bool   `json:"document_server_available"`
	Version                 string `json:"version"`
	BridgeProtocol          int    `json:"bridge_protocol"`
	Toolset                 string `json:"toolset"`
	ToolsRegistered         int    `json:"tools_registered"`
	PythonCapabilities      int    `json:"python_capabilities"`
	StorageWritable         bool   `json:"storage_writable"`
	AllowedRoots            int    `json:"allowed_roots"`
}

// Health builds the current readiness snapshot. It never fails: each flag
// degrades independently so the core stays queryable without backends.
func (h *ToolHandlers) Health() Health {
	toolset := h.toolset
	if toolset == "" {
		toolset = config.ToolsetLegacy
	}
	defs := h.Definitions()
	if toolset == config.ToolsetProfessional {
		defs = FilterProfessionalTools(defs)
	}

	var docServer bool
	if h.bridge != nil {
		docServer = h.bridge.IsConnected()
	}
	var pyCaps int
	if h.pyBridge != nil {
		pyCaps = len(h.pyBridge.Capabilities())
	}

	return Health{
		CoreReady:               true,
		PythonAvailable:         h.pythonAvailable,
		DesktopAvailable:        h.desktopAvailable,
		BuilderAvailable:        h.builder != nil && h.builder.Available(),
		DocumentServerAvailable: docServer,
		Version:                 version.Version,
		BridgeProtocol:          version.BridgeProtocol,
		Toolset:                 toolset,
		ToolsRegistered:         len(defs),
		PythonCapabilities:      pyCaps,
		StorageWritable:         h.storageWritable(),
		AllowedRoots:            len(h.effectiveRoots()),
	}
}

// storageWritable probes the storage dir with a temp file (created+removed).
func (h *ToolHandlers) storageWritable() bool {
	if h.store == nil {
		return false
	}
	f, err := os.CreateTemp(h.store.BaseDir(), ".health-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	if err := os.Remove(name); err != nil {
		return false
	}
	return true
}

// handleHealth serves the Health snapshot as raw JSON (not compacted, so
// false flags stay visible to the caller).
func (h *ToolHandlers) handleHealth(_ context.Context, _ map[string]any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(h.Health())
	if err != nil {
		return textError("health: " + err.Error()), nil
	}
	return textResult(string(b)), nil
}

// requirePythonEngine returns a tool error when the python backend was not
// configured at startup, nil when Execute may proceed. It gates on the
// stable startup flag (engine started and reported capabilities), never on
// the live process: a timeout or cancel kills the process but the flag stays
// true and Execute lazily restarts it. Handlers must call it before touching
// pyBridge.
func (h *ToolHandlers) requirePythonEngine() *mcp.CallToolResult {
	if h.pyBridge == nil || !h.pythonAvailable {
		return textError("python engine unavailable: python engine backend is not available (runner.py missing or failed to start)")
	}
	return nil
}
