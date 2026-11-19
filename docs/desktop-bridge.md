# Desktop bridge

How the MCP server talks to ONLYOFFICE Desktop Editors, what is verified, and
what is still missing. Status: Phase B, protocol v2 in place; pairing hardening
and document binding are Phase B2.

## Topology

```
MCP client ──stdio──> mcp-onlyoffice (Go)
                        │  HTTP 127.0.0.1:<port>  /ping /pair /plugin
                        └──WebSocket──> plugin page inside every editor window
```

The plugin is installed by `plugin.InstallPluginFiles()` into the editor's
`sdkjs-plugins/<GUID>/` directory as `config.json` + `index.html`. The page is
generated in Go (`pluginIndexHTML`), not shipped as a static asset.

## Discovery and pairing

1. The plugin probes `http://127.0.0.1:2021..2070/ping` and only connects to a
   process that answers `{"app":"mcp-onlyoffice"}`.
2. It calls `GET /pair` on that port. `/pair` refuses any request whose `Host`
   is not loopback and whose `Origin` is a browser page (absent, `null`,
   `file://` and `onlyoffice://plugin` are accepted). It answers with the
   per-launch token and the real port.
3. It opens `ws://127.0.0.1:<port>/plugin?token=<token>` and performs the
   handshake below.

The token is 32 random bytes (hex) generated per server launch, persisted 0600
at `~/.config/mcp-onlyoffice/bridge-token` so an operator can read it. It is
never written to the repository or to logs.

**Gap (Phase B2):** `/pair` still hands the token to any local process that can
reach loopback without a browser Origin. A nonce challenge and a
user-confirmed pairing code are the next step. Today the security boundary is
"same OS user, loopback only, no browser page".

## Protocol v2

All frames are single JSON objects over one WebSocket.

```jsonc
// plugin -> server, first frame, within 5 s
{"type":"hello","protocol":2,"plugin_instance_id":"<uuid>","editor_type":"word",
 "capabilities":["reader.selected_text","writer.insert_text"],
 "client_version":"2.1.0"}

// server -> plugin
{"type":"hello_ack","protocol":2,"session_id":"<opaque hex>",
 "server_version":"2.1.0",
 "limits":{"max_frame_bytes":1048576,"request_timeout_ms":10000}}

// server -> plugin
{"type":"command","id":"<32 hex from crypto/rand>","method":"insert_text",
 "params":{"text":"..."},"deadline_ms":10000}

// plugin -> server
{"type":"response","id":"<same id>","ok":true,"result":"ok"}
{"type":"response","id":"<same id>","ok":false,
 "error":{"code":"...","message":"..."}}
```

Rules enforced by the server:

- First frame must be `hello` within 5 s, else the socket is closed with a
  policy violation.
- A `protocol` other than 2 closes the socket; the session is not created.
- `plugin_instance_id` is required; a frame without it is rejected.
- Frames above 1 MiB close the socket (`SetReadLimit`).
- Command ids are cryptographically random, never timestamps.
- A response whose id is not pending is unsolicited: logged, counted, ignored.
  It is never matched to another request.
- A second response for a completed id is likewise ignored.
- Writes to one socket are serialised; websocket writes are not concurrent-safe.

## Sessions

`plugin.Registry` tracks up to 8 concurrent sessions (one editor window each):

| Field | Source |
|---|---|
| `ID` | server-generated, opaque |
| `PluginInstanceID` | plugin-generated per page load |
| `EditorType` | `hello` (`word`, `cell`, `slide`, `pdf`) |
| `Capabilities` | advertised by the plugin, intersected with the method map |
| `DocumentID` | opaque, empty until bound (not implemented yet) |
| `Revision` | server-side counter, incremented after an accepted mutation |
| `ConnectedAt` | handshake time |
| `Active` | the most recently connected or used session |

`list_editor_sessions` returns a snapshot: ids, editor type, sorted
capabilities, document id, revision and the active flag. No addresses and no
tokens.

Legacy editor tools (`get_selected_text`, `insert_text`, …) target the active
session. A command a session does not advertise is refused with
`unsupported_capability` before anything is written to the socket.

## Capabilities

| Method | Capability | word | cell | slide |
|---|---|---|---|---|
| `get_selected_text` | `reader.selected_text` | yes | yes | yes |
| `get_all_content` | `reader.full_content` | yes | no | no |
| `insert_text` | `writer.insert_text` | yes | yes | yes |
| `replace_text` | `writer.replace_text` | yes | no | no |
| `create_table` | `writer.create_table` | yes | yes | yes |
| `create_list` | `writer.create_list` | yes | yes | yes |

`reader.full_content` uses the Office API `ApiDocument.GetAllContent()`
inside `callCommand`, which the installed 9.0.4 word engine provides. It is not
a plugin `executeMethod`, and it does not exist for cell or slide documents,
so those sessions do not advertise it.

The plugin dispatches on a closed `switch` of method names. There is no path
from model text to JavaScript, macros or arbitrary `callCommand` source.

## Errors

`plugin.RPCError` carries a code, a human message, `Retriable` and
`OutcomeUnknown`:

| Code | Meaning |
|---|---|
| `plugin_not_connected` | no session or the socket is gone |
| `session_not_found` | explicit session id does not exist |
| `unsupported_capability` | the session cannot perform that method |
| `protocol_mismatch` | handshake protocol differs |
| `invalid_frame` | malformed frame during handshake |
| `timeout` | no response, nothing was written or the deadline already passed |
| `outcome_unknown` | bytes were written before the failure; the mutation may have happened |
| `plugin_error` | the plugin returned `ok:false` |

`outcome_unknown` is the important one: a timeout or disconnect after the
command frame was written must never be reported as success or as a definite
failure. Reconciling the document state is Phase C.

## Verification status

Verified by automated tests (`internal/plugin/session_test.go`): handshake
success, no-hello close, wrong-protocol close, two concurrent sessions with
isolated results, unsolicited and duplicate response handling, oversized frame
rejection, disconnect during a pending command (`OutcomeUnknown`), capability
refusal, session cap.

Verified against a real process: a Node WebSocket client performing
`/pair` → `hello` → `hello_ack` (protocol 2, limits, session id), and a
go-sdk MCP client calling `health` and `list_editor_sessions` over stdio.
The generated plugin page passes `node --check`.

**Not verified:** the handshake running inside a real Desktop Editors window.
The page's `hello` path and the editor's WebSocket permissions under
`file://` are only checked syntactically and by unit tests. This must be
confirmed interactively before any capability claimed here is advertised as
working on a real document.
