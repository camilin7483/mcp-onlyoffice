package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/cam/mcp-onlyoffice/internal/version"
)

const pluginGUID = "asc.{B0A5F3C2-6A5D-4F0B-9D71-2C3B4E5F6071}"

// Desktop Editors loads <guid-without-asc-prefix>/index.html.
const pluginFolder = "{B0A5F3C2-6A5D-4F0B-9D71-2C3B4E5F6071}"

// pluginVersion must match BridgeVersion and the MCP server version.
// It aliases version.Version, the single source of truth.
const pluginVersion = version.Version

func PluginDir() (string, error) {
	// Only the Linux path below was ever verified. Other platforms must
	// pass an explicit directory with -plugin-dir instead.
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("no verified plugin dir on %s: pass -plugin-dir <dir>", runtime.GOOS)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "onlyoffice", "desktopeditors", "sdkjs-plugins", pluginFolder), nil
}

// ResolvePluginDir returns the override when set (flag wins over config),
// otherwise the verified platform default from PluginDir.
func ResolvePluginDir(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return override, nil
	}
	return PluginDir()
}

func InstallPluginFiles() error {
	dir, err := PluginDir()
	if err != nil {
		return err
	}
	return InstallPluginFilesTo(dir)
}

// InstallPluginFilesTo writes config.json + index.html into dir, creating
// it when possible. Mkdir/write failures name the -plugin-dir override.
func InstallPluginFilesTo(dir string) error {
	return InstallPluginFilesForDesktop(dir, "", 0)
}

// InstallPluginFilesForDesktop uses the SDK shipped with the trusted editor.
func InstallPluginFilesForDesktop(dir, editor string, port int) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("empty plugin dir: pass -plugin-dir <dir>")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir plugin dir %s (override with -plugin-dir): %w", dir, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(pluginConfigJSON()), 0644); err != nil {
		return fmt.Errorf("write config.json in %s (override with -plugin-dir): %w", dir, err)
	}

	page := pluginIndexHTML()
	if port > 0 && port <= 65535 {
		page = strings.Replace(page, "PORT_START = 2021", "PORT_START = "+strconv.Itoa(port), 1)
	}
	if editor != "" {
		if real, err := filepath.EvalSymlinks(editor); err == nil {
			sdkDir := filepath.Dir(real)
			if runtime.GOOS == "linux" && (real == "/usr/bin/desktopeditors" || real == "/usr/local/bin/desktopeditors") {
				sdkDir = "/opt/onlyoffice/desktopeditors"
			}
			sdk := filepath.Join(sdkDir, "editors", "sdkjs-plugins", "v1", "plugins.js")
			if data, err := os.ReadFile(sdk); err == nil {
				if err := os.WriteFile(filepath.Join(dir, "plugins.js"), data, 0644); err != nil {
					return fmt.Errorf("install editor SDK: %w", err)
				}
				page = strings.Replace(page, "../v1/plugins.js", "./plugins.js", 1)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0644); err != nil {
		return fmt.Errorf("write index.html in %s (override with -plugin-dir): %w", dir, err)
	}

	return nil
}

// pluginConfigJSON builds config.json without touching the filesystem,
// so tests can assert on it without writing to $HOME.
func pluginConfigJSON() string {
	return `{
  "name": "MCP Bridge",
  "nameLocale": {
    "en": "MCP Bridge"
  },
  "guid": "` + pluginGUID + `",
  "version": "` + pluginVersion + `",
  "minVersion": "7.0",
  "description": "Bridge plugin for MCP server communication",
  "baseUrl": "",
  "onlyofficeScheme": true,
  "variations": [
    {
      "description": "Background bridge for MCP server",
      "url": "index.html",
      "icons": [],
      "isViewer": false,
      "EditorsSupport": ["word", "cell", "slide", "pdf"],
      "isSystem": true,
      "isDisplayedInViewer": false,
      "initOnSelectionChanged": false,
      "initDataType": "none",
      "initOnConnection": true
    }
  ]
}
`
}

// pluginIndexHTML is the embedded bridge page. Kept in a function so tests
// can assert on its content (no full-document paste path, /ping discovery).
// The connect path is versioned: hello after open, wait for hello_ack,
// then process command frames on the closed switch below.
func pluginIndexHTML() string {
	return `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<script src="../v1/plugins.js"></script>
<script>
let ws = null;
let ready = false;
let connecting = false;
let reconnectTimer = null;
let helloTimer = null;
let activeCommand = null;
let pluginApi = null; // captured at init; the host may clear the global afterwards
let portOffset = 0;
let backoffMs = 1000;
const PORT_START = 2021;
const PORT_SCAN_MAX = 50;
const BACKOFF_MAX_MS = 30000;
const PROTOCOL = 2;
const CLIENT_VERSION = "` + pluginVersion + `";

// Capability set the server enforces per method. Only the editor type that
// actually implements a method advertises it: the paragraph reader
// is implemented only for word, so only word claims
// reader.full_content and the methods built on it.
function capabilitiesFor(editorType) {
  switch (editorType) {
    case "word":
      return ["reader.selected_text", "reader.full_content",
        "writer.insert_text", "writer.replace_text",
        "writer.create_table", "writer.create_list"];
    case "cell":
    case "slide":
      return ["reader.selected_text", "writer.insert_text"];
    default:
      return ["reader.selected_text"];
  }
}

function editorType() {
  try {
    var api = pluginApi || Asc.plugin;
    var t = api && api.info && api.info.editorType;
    if (t === "word" || t === "cell" || t === "slide" || t === "pdf") return t;
  } catch (e) {}
  return "unknown";
}

function pluginInstanceId() {
  try {
    if (typeof crypto !== "undefined" && crypto.randomUUID) return crypto.randomUUID();
  } catch (e) {}
  return "id-" + Math.random().toString(16).slice(2) + Date.now().toString(16);
}

// Probe each candidate port over HTTP and only talk to the one that
// identifies itself as mcp-onlyoffice. Never connect blindly: another
// process may own the port.
async function findServerPort() {
  for (let i = 0; i < PORT_SCAN_MAX; i++) {
    const port = PORT_START + ((portOffset + i) % PORT_SCAN_MAX);
    try {
      const res = await fetch("http://127.0.0.1:" + port + "/ping", { signal: AbortSignal.timeout(500) });
      if (!res.ok) continue;
      const info = await res.json();
      if (info && info.app === "mcp-onlyoffice") {
        portOffset = (port - PORT_START + 1) % PORT_SCAN_MAX;
        return port;
      }
    } catch (e) {
      // Not ours or not listening; try the next port.
    }
  }
  return null;
}

async function pair(port) {
  const chRes = await fetch("http://127.0.0.1:" + port + "/pair/challenge", { signal: AbortSignal.timeout(3000) });
  if (!chRes.ok) throw new Error("challenge rejected: HTTP " + chRes.status);
  const ch = await chRes.json();
  if (!ch || !ch.nonce) throw new Error("challenge returned no nonce");
  const res = await fetch("http://127.0.0.1:" + port + "/pair?nonce=" + encodeURIComponent(ch.nonce), { signal: AbortSignal.timeout(3000) });
  if (!res.ok) throw new Error("pair rejected: HTTP " + res.status);
  return res.json();
}

function scheduleReconnect() {
  ready = false;
  if (helloTimer) { clearTimeout(helloTimer); helloTimer = null; }
  if (ws) { ws.onclose = null; ws.onerror = null; try { ws.close(); } catch (e) {} ws = null; }
  if (reconnectTimer !== null) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, backoffMs);
  backoffMs = Math.min(backoffMs * 2, BACKOFF_MAX_MS);
}

async function connect() {
  if (ws || connecting || reconnectTimer !== null || !captureApi() || editorType() === "unknown") return;
  connecting = true;
  try {
    await openConnection();
  } catch (err) {
    console.error("[MCP] connect failed:", err);
    scheduleReconnect();
  } finally {
    connecting = false;
  }
}

async function openConnection() {
  const port = await findServerPort();
  if (port === null) { scheduleReconnect(); return; }
  let info;
  try {
    info = await pair(port);
  } catch (err) {
    console.error("[MCP] pair failed:", err);
    scheduleReconnect();
    return;
  }
  if (!info || !info.token) {
    console.error("[MCP] pair returned no token");
    scheduleReconnect();
    return;
  }
  const url = "ws://127.0.0.1:" + port + "/plugin?token=" + encodeURIComponent(info.token);
  ws = new WebSocket(url);
  ws.onopen = () => {
    console.log("[MCP] connected on port " + port);
    backoffMs = 1000;
    // Versioned handshake: hello first, commands only after hello_ack.
    var et = editorType();
    ws.send(JSON.stringify({
      type: "hello",
      protocol: PROTOCOL,
      plugin_instance_id: pluginInstanceId(),
      editor_type: et,
      capabilities: capabilitiesFor(et),
      client_version: CLIENT_VERSION
    }));
    if (helloTimer) clearTimeout(helloTimer);
    helloTimer = setTimeout(() => {
      console.error("[MCP] no hello_ack, reconnecting");
      scheduleReconnect();
    }, 5000);
  };
  ws.onmessage = (e) => {
    let msg;
    try {
      msg = JSON.parse(e.data);
    } catch (err) {
      console.error("[MCP] parse error:", err);
      return;
    }
    if (!ready) {
      if (msg && msg.type === "hello_ack" && msg.protocol === PROTOCOL) {
        ready = true;
        if (helloTimer) { clearTimeout(helloTimer); helloTimer = null; }
        console.log("[MCP] session " + msg.session_id);
      }
      return;
    }
    if (msg && msg.type === "command") {
      // El SDK comparte Asc.scope y callbacks entre comandos.
      if (activeCommand !== null) {
        sendResponse(msg.id, null, "editor is busy processing another command");
        return;
      }
      activeCommand = { id: msg.id, socket: ws };
      try {
        handleCommand(msg);
      } catch (err) {
        console.error("[MCP] command error:", err);
        sendResponse(msg.id, null, "editor command failed");
      }
    }
  };
  ws.onclose = () => {
    console.log("[MCP] disconnected from port " + port);
    ws = null;
    scheduleReconnect();
  };
  ws.onerror = () => {
    if (ws) ws.close();
  };
}

function sendResponse(id, result, error) {
  let socket = ws;
  if (activeCommand !== null && activeCommand.id === id) {
    socket = activeCommand.socket;
    activeCommand = null;
  }
  let msg;
  if (error === undefined || error === null) {
    msg = { type: "response", id: id, ok: true, result: (result === undefined ? null : result) };
  } else if (typeof error === "string") {
    msg = { type: "response", id: id, ok: false, error: { code: "plugin_error", message: error } };
  } else {
    msg = { type: "response", id: id, ok: false,
      error: { code: error.code || "plugin_error", message: error.message || "plugin error" } };
  }
  if (socket && socket.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify(msg));
  }
}

function handleCommand(msg) {
  var id = msg.id, method = msg.method, params = msg.params || {};
  switch (method) {
    case "ping":
      sendResponse(id, "pong");
      break;
    case "get_selected_text":
      execMethod(id, "GetSelectedText", [], function(res) { sendResponse(id, res); });
      break;
    case "get_all_content":
      runCommand(id, function() {
        try {
          var paragraphs = Api.GetDocument().GetAllParagraphs();
          return paragraphs.map(function(paragraph) { return paragraph.GetText(); }).join("\n");
        } catch (err) { return null; }
      }, false, function(text) { sendResponse(id, text); });
      break;
    case "insert_text":
      execMethod(id, "PasteText", [params.text || ""], function() { sendResponse(id, "ok"); });
      break;
    case "replace_text":
      if (!params.old) { sendResponse(id, null, "old text is required"); return; }
      Asc.scope = { oldText: params.old, newText: params.new || "" };
      runCommand(id, function() {
        try {
          var doc = Api.GetDocument();
          var count = doc.Search(Asc.scope.oldText, true).length;
          if (count && !doc.SearchAndReplace({ searchString: Asc.scope.oldText,
            replaceString: Asc.scope.newText, matchCase: true })) return null;
          return count;
        } catch (err) { return null; }
      }, true, function(count) { sendResponse(id, { matches: count, changed: count > 0 }); });
      break;
    case "create_table":
      Asc.scope = { rows: params.rows || 3, cols: params.cols || 3 };
      runCommand(id, function() {
        try {
          var table = Api.CreateTable(Asc.scope.cols, Asc.scope.rows);
          Api.GetDocument().InsertContent([table]);
          return true;
        } catch (err) { return null; }
      }, true, function() { sendResponse(id, "ok"); });
      break;
    case "create_list":
      Asc.scope = { items: params.items || ["Item 1"], listType: params.type || "bullet" };
      runCommand(id, function() {
        try {
          var doc = Api.GetDocument();
          var numbering = doc.CreateNumbering(Asc.scope.listType === "numbered" ? "numbered" : "bullet");
          var level = numbering.GetLevel(0);
          var paragraphs = Asc.scope.items.map(function(item) {
            var paragraph = Api.CreateParagraph();
            paragraph.SetNumbering(level);
            paragraph.AddText(item);
            return paragraph;
          });
          doc.InsertContent(paragraphs);
          return true;
        } catch (err) { return null; }
      }, true, function() { sendResponse(id, "ok"); });
      break;
    case "set_bold":
      execMethod(id, "SetBold", [params.value !== false], function() { sendResponse(id, "ok"); });
      break;
    case "set_italic":
      execMethod(id, "SetItalic", [params.value !== false], function() { sendResponse(id, "ok"); });
      break;
    case "get_document_info":
      sendResponse(id, { editorType: editorType() });
      break;
    default:
      sendResponse(id, null, "unknown method: " + method);
  }
}

function execMethod(id, name, args, cb) {
  try {
    if (pluginApi.executeMethod(name, args, cb) === false) {
      sendResponse(id, null, "editor rejected " + name);
    }
  } catch (err) {
    sendResponse(id, null, "editor could not execute " + name);
  }
}

function runCommand(id, fn, recalculate, cb) {
  try {
    pluginApi.callCommand(fn, false, recalculate, function(result) {
      if (result == null || (typeof result === "object" && result.onlyoffice_id_error_message)) {
        sendResponse(id, null, "editor command failed");
        return;
      }
      cb(result);
    });
  } catch (err) {
    sendResponse(id, null, "editor could not execute command");
  }
}

function captureApi() {
  if (typeof Asc !== "undefined" && Asc && Asc.plugin &&
      typeof Asc.plugin.executeMethod === "function") {
    pluginApi = Asc.plugin;
  }
  return pluginApi;
}

if (typeof Asc !== "undefined" && Asc && Asc.plugin) {
  Asc.plugin.init = function () {
    captureApi();
    console.log("[MCP] plugin init");
    connect();
  };
  Asc.plugin.onDocumentOpen = function () {
    console.log("[MCP] document opened");
  };
}

// Connect even if the host never calls Asc.plugin.init (background/system
// plugin); capture the API when it becomes available.
function ensureConnected() {
  captureApi();
  if (!ws || ws.readyState === WebSocket.CLOSED || ws.readyState === WebSocket.CLOSING) {
    connect();
  }
}
setTimeout(ensureConnected, 500);
setInterval(ensureConnected, 5000);
</script>
</head>
<body></body>
</html>
`
}
