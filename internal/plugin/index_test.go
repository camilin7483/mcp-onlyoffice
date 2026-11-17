package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	// Full-content reads feed get_all_content and the before/after match check;
	// they never feed PasteText.
	if strings.Count(html, "GetAllContent") != 3 {
		t.Errorf("GetAllContent appears %d times, want 3 (get + replace before/after)",
			strings.Count(html, "GetAllContent"))
	}
	if !strings.Contains(html, "matches") {
		t.Error("replace_text must report a match count")
	}
	if !strings.Contains(html, "setInterval(ensureConnected") || !strings.Contains(html, "captureApi") {
		t.Error("plugin must auto-connect and capture Asc.plugin without relying on host init")
	}
}

func TestManifestVersionAndGUID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := InstallPluginFiles(); err != nil {
		t.Fatalf("InstallPluginFiles: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".local", "share", "onlyoffice",
		"desktopeditors", "sdkjs-plugins", pluginFolder, "config.json"))
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
	if cfg["version"] != BridgeVersion || BridgeVersion != "2.0.0" {
		t.Fatalf("version = %v, want 2.0.0 matching BridgeVersion", cfg["version"])
	}
}
