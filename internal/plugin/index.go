package plugin

import (
	"fmt"
	"os"
	"path/filepath"
)

const pluginGUID = "asc.MCP-Bridge"

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
  "version": "1.0.0",
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

	indexHTML := `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<script>
let ws = null;
let retryTimer = null;
let portIndex = 0;
const PORT_START = 2021;
const PORT_SCAN_MAX = 100;

function connect() {
  if (ws && ws.readyState === WebSocket.OPEN) return;
  if (ws) { ws.onclose = null; ws.onerror = null; ws.close(); ws = null; }

  const port = PORT_START + portIndex;
  const url = "ws://127.0.0.1:" + port + "/plugin";
  ws = new WebSocket(url);
  ws.onopen = () => {
    console.log("[MCP] connected on port " + port);
    portIndex = 0;
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
    portIndex = (portIndex + 1) % PORT_SCAN_MAX;
    setTimeout(connect, portIndex === 0 ? 5000 : 500);
  };
  ws.onerror = () => {
    ws && ws.close();
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
      execMethod("GetAllContent", [], function(content) {
        var newContent = (content || "").split(params.old || "").join(params.new || "");
        execMethod("PasteText", [newContent], function() { sendResponse(id, "ok"); });
      });
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
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(indexHTML), 0644); err != nil {
		return fmt.Errorf("write index.html: %w", err)
	}

	return nil
}
