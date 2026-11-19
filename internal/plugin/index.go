package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("empty plugin dir: pass -plugin-dir <dir>")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir plugin dir %s (override with -plugin-dir): %w", dir, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(pluginConfigJSON()), 0644); err != nil {
		return fmt.Errorf("write config.json in %s (override with -plugin-dir): %w", dir, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(pluginIndexHTML()), 0644); err != nil {
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
<script>
let ws = null;
let ready = false;
let helloTimer = null;
let pluginApi = null; // captured at init; the host may clear the global afterwards
let portOffset = 0;
let backoffMs = 1000;
const PORT_START = 2021;
const PORT_SCAN_MAX = 50;
const BACKOFF_MAX_MS = 30000;
const PROTOCOL = 2;
const CLIENT_VERSION = "` + pluginVersion + `";

// Capability set the server enforces per method. Only the editor type that
// actually implements a method advertises it: the full-content Office API
// call inside callCommand exists only in word, so only word claims
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
    var t = Asc && Asc.plugin && Asc.plugin.info && Asc.plugin.info.editorType;
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
      const res = await fetch("http://127.0.0.1:" + port + "/ping");
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
  const chRes = await fetch("http://127.0.0.1:" + port + "/pair/challenge");
  if (!chRes.ok) throw new Error("challenge rejected: HTTP " + chRes.status);
  const ch = await chRes.json();
  if (!ch || !ch.nonce) throw new Error("challenge returned no nonce");
  const res = await fetch("http://127.0.0.1:" + port + "/pair?nonce=" + encodeURIComponent(ch.nonce));
  if (!res.ok) throw new Error("pair rejected: HTTP " + res.status);
  return res.json();
}

function scheduleReconnect() {
  ready = false;
  if (helloTimer) { clearTimeout(helloTimer); helloTimer = null; }
  if (ws) { ws.onclose = null; ws.onerror = null; try { ws.close(); } catch (e) {} ws = null; }
  setTimeout(connect, backoffMs);
  backoffMs = Math.min(backoffMs * 2, BACKOFF_MAX_MS);
}

async function connect() {
  if (ws && ws.readyState === WebSocket.OPEN) return;
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
      try {
        handleCommand(msg);
      } catch (err) {
        console.error("[MCP] command error:", err);
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
  let msg;
  if (error === undefined || error === null) {
    msg = { type: "response", id: id, ok: true, result: (result === undefined ? null : result) };
  } else if (typeof error === "string") {
    msg = { type: "response", id: id, ok: false, error: { code: "plugin_error", message: error } };
  } else {
    msg = { type: "response", id: id, ok: false,
      error: { code: error.code || "plugin_error", message: error.message || "plugin error" } };
  }
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(msg));
  }
}

function handleCommand(msg) {
  var id = msg.id, method = msg.method, params = msg.params || {};
  switch (method) {
    case "ping":
      sendResponse(id, "pong");
      break;
    case "get_selected_text":
      execMethod("GetSelectedText", [], function(res) { sendResponse(id, res); });
      break;
    case "get_all_content":
      callCommandResult(function() { return Api.GetDocument().GetAllContent(); },
        function(res) { sendResponse(id, res); });
      break;
    case "insert_text":
      execMethod("PasteText", [params.text || ""], function() { sendResponse(id, "ok"); });
      break;
    case "replace_text":
      // Truthful find-and-replace via the SDK's SearchAndReplace method
      // (verified at https://api.onlyoffice.com: executeMethod
      // "SearchAndReplace" with {searchString, replaceString}). The SDK
      // returns no match count, so count occurrences of the search string
      // in the current content first and report that number. Never paste
      // the whole document.
      (function() {
        var oldT = params.old || "";
        var newT = params.new || "";
        if (!oldT) { sendResponse(id, null, "old text is required"); return; }
        callCommandResult(function() { return Api.GetDocument().GetAllContent(); }, function(content) {
          var text = (content === null || content === undefined) ? "" : String(content);
          var matches = text.split(oldT).length - 1;
          execMethod("SearchAndReplace", [{ searchString: oldT, replaceString: newT }], function() {
            callCommandResult(function() { return Api.GetDocument().GetAllContent(); }, function(after) {
              var result = (after === null || after === undefined) ? "" : String(after);
              sendResponse(id, { matches: matches, changed: matches > 0 && result.indexOf(newT) >= 0 });
            });
          });
        });
      })();
      break;
    case "create_table":
      callCommand(function(resolve) {
        var doc = Api.GetDocument();
        var table = Api.CreateTable(params.rows || 3, params.cols || 3);
        doc.InsertContent([table]);
        resolve();
      }, function() { sendResponse(id, "ok"); });
      break;
    case "create_list":
      var items = params.items || ["Item 1"];
      var listType = params.type || "bullet";
      callCommand(function(resolve) {
        var doc = Api.GetDocument();
        var list;
        if (listType === "numbered") {
          list = Api.CreateNumberedList("numbers");
        } else {
          list = Api.CreateBulletList("bullet");
        }
        for (var i = 0; i < items.length; i++) {
          var para = Api.CreateParagraph();
          para.AddText(items[i]);
          list.AddParagraph(para);
        }
        doc.InsertContent([list]);
        resolve();
      }, function() { sendResponse(id, "ok"); });
      break;
    case "set_bold":
      execMethod("SetBold", [params.value !== false], function() { sendResponse(id, "ok"); });
      break;
    case "set_italic":
      execMethod("SetItalic", [params.value !== false], function() { sendResponse(id, "ok"); });
      break;
    case "get_document_info":
      sendResponse(id, { editorType: "unknown" });
      break;
    default:
      sendResponse(id, null, "unknown method: " + method);
  }
}

function execMethod(name, args, cb) {
  var api = pluginApi || (typeof Asc !== "undefined" ? Asc.plugin : null);
  if (!api || typeof api.executeMethod !== "function") {
    console.error("[MCP] plugin API unavailable for " + name);
    if (cb) cb(null);
    return;
  }
  try {
    api.executeMethod(name, args, cb || function(){});
  } catch (err) {
    console.error("[MCP] executeMethod error:", err);
    if (cb) cb(null);
  }
}

function callCommandResult(fn, cb) {
  var api = pluginApi || (typeof Asc !== "undefined" ? Asc.plugin : null);
  if (!api || typeof api.callCommand !== "function") {
    console.error("[MCP] plugin API unavailable for callCommand");
    if (cb) cb(null);
    return;
  }
  try {
    api.callCommand("(" + fn.toString() + ")();", false, true, cb || function(){});
  } catch (err) {
    console.error("[MCP] callCommand error:", err);
    if (cb) cb(null);
  }
}

function callCommand(fn, cb) {
  var api = pluginApi || (typeof Asc !== "undefined" ? Asc.plugin : null);
  if (!api || typeof api.callCommand !== "function") {
    console.error("[MCP] plugin API unavailable for callCommand");
    if (cb) cb();
    return;
  }
  try {
    var s = fn.toString();
    api.callCommand("(" + s + ")(function(){});", false, true, cb || function(){});
  } catch (err) {
    console.error("[MCP] callCommand error:", err);
    if (cb) cb();
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
