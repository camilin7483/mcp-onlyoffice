# Prior art: related projects studied

No code was copied from any project below. Ideas were studied from public
descriptions and repository contents only; every implementation in this
tree was written here. License identifiers were confirmed via the GitHub
API on 2026-11-19; where the API reports no license, the table says
`not confirmed` instead of guessing.

| Project | URL | License (GitHub API) | Idea studied | Code reused |
|---|---|---|---|---|
| ONLYOFFICE/docspace-mcp | https://github.com/ONLYOFFICE/docspace-mcp | MIT | Official DocSpace MCP server: how ONLYOFFICE scopes a server to the DocSpace API rather than the desktop editors | no |
| ONLYOFFICE/docs-mcp | https://github.com/ONLYOFFICE/docs-mcp | MIT | Official Docs MCP server: reference for which operations ONLYOFFICE itself exposes over MCP | no |
| s-b-repo/onlyoffice-mcp-server | https://github.com/s-b-repo/onlyoffice-mcp-server | MIT | Community Python server (77 async tools, version history, charts, spell-check, find/replace, security layers): breadth-first tool-surface design | no |
| maximlefebvre/onlyoffice-claude-bridge | https://github.com/maximlefebvre/onlyoffice-claude-bridge | not confirmed (API reports no license; no LICENSE file at the repo root as of 2026-11-19) | Unofficial plugin + local MCP server that lets Claude edit the open document: precedent for the bridge-plugin architecture | no |
| Rlhoste/Onlyoffice-MCP | https://github.com/Rlhoste/Onlyoffice-MCP | GPL-3.0 | Presentation-focused bridge editing the open document in place instead of regenerating files: in-place-edit motivation | no |
| matheusrosafogo/onlyoffice-opencode | https://github.com/matheusrosafogo/onlyoffice-opencode | MIT | OpenCode plugin for Desktop Editors plus a local bridge: client-configuration pattern | no |

## Attribution notes

- The desktop-bridge approach (a background system plugin opening a
  loopback WebSocket to a local server, `internal/plugin/index.go`) follows
  the same shape as maximlefebvre's bridge and matheusrosafogo's OpenCode
  plugin at the architectural level only; the pairing protocol (nonce
  challenge, per-launch token), the versioned hello handshake, and all Go
  and JavaScript code here are original to this repository.
- The GPL-3.0 project (Rlhoste/Onlyoffice-MCP) was used as a concept
  reference for in-place presentation editing. No GPL-licensed code is
  included, linked, or derived from in this tree, whose license is MIT
  (`LICENSE`).
- If any license above changes upstream, this table is stale: re-check via
  `GET https://api.github.com/repos/<owner>/<repo>` and look at the
  `license.spdx_id` field.
