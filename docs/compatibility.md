# Compatibility and verification

## MCP

The server uses `github.com/modelcontextprotocol/go-sdk v1.8.0`. The release
audit exercised the real stdio transport with a JSON-RPC client, including
initialization, tool discovery, tool calls and shutdown on EOF. It verified
the `2025-11-25` handshake and `2026-07-28` request metadata with
`server/discover`, `tools/list` and `health`. The legacy profile exposed 74
tools and the professional profile exposed 20. The audit did not run Claude
Desktop, OpenCode, Cursor or other assistant clients.

## ONLYOFFICE Desktop Editors

Verified with **9.0.4.50 on Linux/amd64** under an isolated graphical session.
The installed plugin connected to the configured loopback port and identified
Word, spreadsheet and presentation sessions. The audit opened a managed DOCX,
read paragraphs, inserted and replaced text, inserted a table and a numbered
list, saved in Desktop Editors and verified the resulting document through
`export_document`. It generated and opened XLSX and PPTX fixtures and confirmed
that an ambiguous edit was refused with several sessions open.

On the first Desktop open of a managed document, the store moves its legacy
`<id>.data` payload to `<id>.<extension>`. The document keeps its ID. Desktop
saves and storage exports use the same payload. Older server binaries expect
the `.data` layout and should not share the migrated store.

Legacy tool calls without a session ID require at most one connected editor.
For typed Desktop edits with several open editors, obtain the target session
from `list_editor_sessions` and pass its `session_id` to `document_operation`.
The bridge does not bind that session to a managed document ID or path.

The Linux automatic plugin path is
`~/.local/share/onlyoffice/desktopeditors/sdkjs-plugins/<GUID>`. Other platforms
require `-plugin-dir` and, where automatic editor detection is unavailable, a
configured Desktop executable. Binaries were cross-built for Windows and
macOS on amd64 and arm64. Interactive behaviour on those platforms and on
ONLYOFFICE 7.x, 8.x and 10.x remains unverified. A follow-up Linux audit
verified a saved spreadsheet cell edit, retention of numeric formatting and
styles after the save, and a saved PowerPoint title edit in Desktop Editors.
Only the spreadsheet formatting change was issued via MCP; the subsequent
XLSX cell and PPTX title edits were made in the graphical editor.

## Other backends

Document Builder and ONLYOFFICE Document Server were unavailable for this
integration test. Structural OOXML checks in `cmd/bench` cover synthetic
fixtures and measure target-part preservation independently of the graphical
session. Network and multi-user deployment are outside the verified loopback,
single-user setup. See [threat model](threat-model.md).
