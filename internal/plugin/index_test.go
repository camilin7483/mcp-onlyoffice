package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cam/mcp-onlyoffice/internal/version"
)

func TestPluginHTMLDiscovery(t *testing.T) {
	html := pluginIndexHTML()
	for _, want := range []string{"/ping", "/pair", "mcp-onlyoffice", "token", "SearchAndReplace"} {
		if !strings.Contains(html, want) {
			t.Errorf("embedded plugin page should contain %q", want)
		}
	}
	if !strings.Contains(html, "PORT_SCAN_MAX = 50") {
		t.Error("plugin scan must stay bounded to 50 ports")
	}
	if !strings.Contains(html, "PORT_START = 2021") {
		t.Error("plugin scan must start at PORT_START 2021")
	}
}

func TestPluginHTMLNoFullDocumentPaste(t *testing.T) {
	html := pluginIndexHTML()
	for _, bad := range []string{
		"split(params.old",
		"PasteText\", [newContent]",
		"join(params.new",
	} {
		if strings.Contains(html, bad) {
			t.Errorf("embedded page must not paste the whole document (found %q)", bad)
		}
	}
	if !strings.Contains(html, "GetAllParagraphs") || !strings.Contains(html, "callCommand(fn,") {
		t.Error("paragraph reads must call the editor API with a function")
	}
	if !strings.Contains(html, "matches") {
		t.Error("replace_text must report a match count")
	}
	if !strings.Contains(html, "setInterval(ensureConnected") || !strings.Contains(html, "captureApi") {
		t.Error("plugin must auto-connect and capture Asc.plugin without relying on host init")
	}
}

func TestManifestVersionAndGUID(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("default plugin directory is Linux-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := InstallPluginFiles(); err != nil {
		t.Fatalf("InstallPluginFiles: %v", err)
	}
	// Read through PluginDir (not a HOME-joined path): os.UserHomeDir
	// ignores HOME on Windows, so rebuilding the path here would diverge.
	dir, err := PluginDir()
	if err != nil {
		t.Fatalf("PluginDir: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode config.json: %v", err)
	}
	if cfg["guid"] != pluginGUID || !strings.HasPrefix(pluginGUID, "asc.{") {
		t.Fatalf("GUID changed: %v", cfg["guid"])
	}
	if cfg["version"] != version.Version || cfg["version"] != BridgeVersion {
		t.Fatalf("version = %v, want %s matching BridgeVersion", cfg["version"], version.Version)
	}
}

func TestPluginConfigVersion(t *testing.T) {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(pluginConfigJSON()), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg["version"] != version.Version {
		t.Fatalf("version = %v, want %s", cfg["version"], version.Version)
	}
}

func TestPluginInstallerUsesLocalSDKAndConfiguredPort(t *testing.T) {
	base := t.TempDir()
	editor := filepath.Join(base, "DesktopEditors")
	if err := os.WriteFile(editor, nil, 0700); err != nil {
		t.Fatal(err)
	}
	sdkRoot := filepath.Join(base, "editors", "sdkjs-plugins", "v1")
	if err := os.MkdirAll(sdkRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdkRoot, "plugins.js"), []byte("// SDK fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(base, "plugin")
	if err := InstallPluginFilesForDesktop(pluginDir, editor, 28501); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(pluginDir, "index.html"))
	if err != nil || !strings.Contains(string(page), `src="./plugins.js"`) || !strings.Contains(string(page), "PORT_START = 28501") {
		t.Fatalf("generated page did not use the local SDK and configured port: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(pluginDir, "plugins.js"))
	if err != nil || string(data) != "// SDK fixture" {
		t.Fatalf("installed SDK = %q, %v", data, err)
	}
}
