package plugin

import (
	"fmt"
	"os"
	"path/filepath"
)

const pluginGUID = "asc.MCP-Bridge"

// pluginVersion must match BridgeVersion and the MCP server version.
const pluginVersion = "2.0.0"

func PluginDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "onlyoffice", "desktopeditors", "sdkjs-plugins", pluginGUID), nil
}

func InstallPluginFiles() error {
	dir, err := PluginDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir plugin dir: %w", err)
	}

	config := `{
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
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), 0644); err != nil {
		return fmt.Errorf("write config.json: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(pluginIndexHTML()), 0644); err != nil {
		return fmt.Errorf("write index.html: %w", err)
	}

	return nil
}

// pluginIndexHTML is the embedded bridge page. Kept in a function so tests
// can assert on its content (no full-document paste path, /ping discovery).
func pluginIndexHTML() string {
	return `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<script>
let ws = null;
let portOffset = 0;
let backoffMs = 1000;
const PORT_START = 2021;
const PORT_SCAN_MAX = 50;
const BACKOFF_MAX_MS = 30000;

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
  const res = await fetch("http://127.0.0.1:" + port + "/pair");
  if (!res.ok) throw new Error("pair rejected: HTTP " + res.status);
  return res.json();
}

function scheduleReconnect() {
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
  };
  ws.onmessage = (e) => {
    try {
      const msg = JSON.parse(e.data);
      handleCommand(msg);
    } catch (err) {
      console.error("[MCP] parse error:", err);
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
  const msg = { id: id, result: result, error: error };
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
      execMethod("GetAllContent", [], function(res) { sendResponse(id, res); });
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
        execMethod("GetAllContent", [], function(content) {
          var text = (content === null || content === undefined) ? "" : String(content);
          var matches = text.split(oldT).length - 1;
          execMethod("SearchAndReplace", [{ searchString: oldT, replaceString: newT }], function() {
            sendResponse(id, { matches: matches });
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
  try {
    Asc.plugin.executeMethod(name, args, cb || function(){});
  } catch (err) {
    console.error("[MCP] executeMethod error:", err);
    if (cb) cb(null);
  }
}

function callCommand(fn, cb) {
  try {
    var s = fn.toString();
    Asc.plugin.callCommand("(" + s + ")(function(){});", false, true, cb || function(){});
  } catch (err) {
    console.error("[MCP] callCommand error:", err);
    if (cb) cb();
  }
}

Asc.plugin.init = function () {
  console.log("[MCP] plugin init");
  connect();
};

Asc.plugin.onDocumentOpen = function () {
  console.log("[MCP] document opened");
};
</script>
</head>
<body></body>
</html>
`
}
