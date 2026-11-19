# Audit baseline

Audit performed before any change, on commit `2396e7a` (`main`, 9 commits ahead of
`origin/main`), working tree dirty with uncommitted documentation edits.

Every statement below is tagged:

- **VERIFIED** — reproduced by running code or reading the cited file.
- **DECLARED** — stated by README/CHANGELOG/docs but not reproduced.
- **NOT VERIFIED** — plausible, not checked.
- **BLOCKED** — cannot be checked in this environment.

## 1. Environment and toolchain

| Item | Value | Status |
|---|---|---|
| Commit | `2396e7a` on `main` | VERIFIED |
| Branch state | 9 commits ahead of `origin/main`, no tags, no releases | VERIFIED |
| Go module | `github.com/cam/mcp-onlyoffice`, `go 1.25.0` | VERIFIED (`go.mod:1-3`) |
| Go toolchain on host | `go1.27.4-X:nodwarf5 linux/amd64` | VERIFIED |
| MCP SDK | `github.com/modelcontextprotocol/go-sdk v1.6.1` | VERIFIED (`go.mod:8`) |
| WebSocket | `github.com/coder/websocket v1.8.15` | VERIFIED |
| Python on host | 3.14.10 | VERIFIED |
| License | MIT, Copyright (c) 2026 CamiloDev | VERIFIED (`LICENSE:1-3`) |
| CI | none: no `.github/` directory | VERIFIED |

### 1.1 Repository is not buildable from a clean clone

**VERIFIED, critical.** `cmd/mcp-onlyoffice/main.go` (85 lines, the only entrypoint)
has never been committed. The cause is the previous `.gitignore` rule `mcp-onlyoffice`
(no leading slash), which matched the `cmd/mcp-onlyoffice/` directory. The working
tree already contains the fix (`/mcp-onlyoffice`), still uncommitted.

Consequence: `make build`, `go build ./cmd/mcp-onlyoffice` and any CI on a fresh
clone fail. This is the first thing Phase A must land.

### 1.2 Baseline commands

| Command | Result |
|---|---|
| `go build ./...` | PASS (dirty tree, entrypoint present locally) |
| `go vet ./...` | PASS, no output |
| `go test ./...` | PASS, 13 packages, 119 test functions |
| `go test -race ./internal/plugin ./internal/server ./internal/storage` | PASS |
| `pytest` from `python_engine/` | PASS, 15 tests |
| `pytest` from repo root | FAIL: `ImportError: cannot import name 'DocumentEngine'` — no pytest config, tests assume cwd=`python_engine/` |
| Coverage | NOT VERIFIED — never measured |
| `govulncheck` | NOT VERIFIED — not installed, not in CI |

## 2. Architecture as built

```
MCP stdio (go-sdk v1.6.1)
  └─ internal/server (70 tools legacy, 16 professional)
       ├─ internal/storage      document store (UUID ids, allowed-root path resolver)
       ├─ internal/ooxml        DOCX/XLSX/PPTX writers (build from scratch)
       ├─ internal/validator    package validator (structure, XML well-formedness, rels)
       ├─ internal/engine       Python NDJSON bridge (persistent child process)
       ├─ internal/plugin       WebSocket bridge to the ONLYOFFICE Desktop plugin
       ├─ internal/planner      static intent→capability maps
       ├─ internal/workflow     named Go workflows
       ├─ internal/agent        template-driven report builder
       ├─ internal/analyzer     structure/stats reader
       ├─ internal/templates    static template registry
       └─ internal/audit        JSONL audit log
```

Code size: 13,367 lines of Go, 3,318 lines of Python.

**VERIFIED dead or duplicated code** (no production caller found):
`toMCPTools` (`server/discovery.go:89-99`), `replaceTextResult`
(`server/doc_handlers.go:308-317`), `Planner.ExecutePlan` (production uses
`BatchExecute`), `analyzer.DetectInconsistencies` / `SuggestImprovements`,
`templates.RenderText`, `CapabilityIndex.FindByTag`, `EnginePool` /
`NewEnginePool`, most of `SessionCache`, `Improvement.Record`. `sanitizeFilename`
is implemented three times (`server/tools.go:184`, `workflow/workflow.go:326`,
`agent/agent.go:188`). `internal/plugin/data/index.html` is a stale copy that is
not embedded and not shipped; `pluginIndexHTML()` in `internal/plugin/index.go`
is the real source.

## 3. Startup contract

**VERIFIED, blocking Phase A criteria.**

- `server.New` returns an error when ONLYOFFICE Desktop is not found
  (`server/server.go:186-206` collects the failed check, `:132-140` aborts
  startup). The core therefore cannot run headless, which contradicts the
  product claim that the server works without Desktop.
- `server.New` also aborts when the Python engine fails to start, is missing, or
  reports zero capabilities (`server/server.go:70-114`). Python is a hard
  dependency of the whole process, not an optional backend.
- Health is a startup-only, all-or-nothing log line. There is no health
  capability exposed to MCP clients, and no separation between
  `core_ready`, `python_available`, `desktop_available`,
  `builder_available`, `document_server_available`.

## 4. Version consistency

**VERIFIED.** The same version is spelled in four places and drifts:

| Location | Value |
|---|---|
| `server/server.go:229` (`Implementation.Version`) | `2.0.0` |
| `plugin/bridge.go:25` (`BridgeVersion`) | `2.0.0` |
| `plugin/index.go:15` (`pluginVersion`) | `2.0.0` |
| `python_engine/runner.py` (`__version__`) | `1.0.0` |

No single source of truth, no test that keeps them aligned.

## 5. Desktop bridge (`internal/plugin`)

Implemented today (VERIFIED, `plugin/bridge.go`):

- Listens on `127.0.0.1` only, port scan 2021..2070, random fallback port.
- Per-launch 32-byte random token (`newToken`), persisted 0600 at
  `~/.config/mcp-onlyoffice/bridge-token`, accepted as `?token=` or
  `Authorization: Bearer`.
- `/ping` is unauthenticated by design and identifies the app.
- `/pair` hands the token to loopback clients whose `Origin` is absent, `null`,
  `file://` or `onlyoffice://plugin`; browser sites are refused.
- WebSocket handshake validates Origin separately (accepts the custom
  `onlyoffice://plugin` scheme, therefore `InsecureSkipVerify` on the library
  origin check).
- 1 MiB read limit, 32 in-flight request cap, one plugin connection at a time
  (a second client replaces the first).
- Pending requests are failed with `plugin disconnected` when the socket drops.

Missing versus the target model (VERIFIED by absence):

- No protocol version negotiation, no handshake, no `plugin_instance_id`,
  `editor_session_id`, `document_id`, or capability report from the plugin.
- No replay protection (no nonce, no monotonic sequence), request IDs are
  `mcp-<UnixNano>` (`bridge.go:365`), not cryptographically random.
- Errors are free-form strings; there is no typed error and no
  `unknown_outcome` state. A write followed by a timeout is reported as a plain
  error, so success and failure are indistinguishable.
- One implicit "the connected editor". Two open documents cannot be told apart.
- No `expected_revision`; a concurrent edit is silently overwritten.
- No schema validation of plugin responses beyond JSON decoding.

## 6. Desktop editing surface

**VERIFIED.** Six commands: `get_selected_text`, `get_all_content`,
`insert_text`, `replace_text`, `create_table`, `create_list`, plus unused
`set_bold`/`set_italic`/`get_document_info`. The generated plugin dispatches on a
closed `switch` (`plugin/index.go:172-242`); there is no path from model text to
JavaScript, macros, or `callCommand` source. `replace_text` uses the SDK
`SearchAndReplace` method after counting matches; it no longer pastes the whole
document (an earlier revision did).

The plugin cannot yet express: paragraph/run addressing, range selection,
insert before/after, styles, tables beyond insertion, hyperlinks, comments,
content controls, spreadsheet ranges/formulas/formats/named ranges/charts,
presentation shapes/notes/geometry. No capability above is claimed without a
test, and none of the missing ones has one.

## 7. Filesystem sandbox

**VERIFIED good:** one shared resolver (`storage/storage.go:53-131`) does
`Abs`+`Clean`, walks to the deepest existing ancestor with `Lstat`, resolves
symlinks with `EvalSymlinks`, and checks boundary-safe containment in
`allowed_roots` (default: storage dir only). Writes go through `ResolveDest`,
which refuses a symlink leaf. Read cap 32 MiB, write cap 64 MiB, zip 1000
entries / 256 MiB, walk cap 10,000 entries skipping symlinks, overwrite requires
`overwrite=true` and makes a `.bak-<timestamp>` copy.

**VERIFIED defects, ranked:**

1. `delete_file` and `move_file` never compare the resolved path against the
   roots: `recursive=true` on a root runs `os.RemoveAll` on the root
   (`fs_handlers.go:520-541`, `:485-513`).
2. Backup files are written to a predictable `<file>.bak-<timestamp>` path with
   no symlink check (`fs_handlers.go:205`, `doc_handlers.go:31`), so a symlink
   planted there writes outside the roots.
3. TOCTOU: validate, then reopen by path (`fs_handlers.go:267-270`, `:495-509`,
   `:817-829`). A file can grow past the stat cap before the read, and the zip
   destination is re-opened after validation.
4. Hardlinks are never checked (`Nlink`): an in-root hardlink to an outside
   inode passes the path check and gets truncated.
5. `ensureWritable` and `import_document` read the whole existing file with no
   size limit (`fs_handlers.go:201`, `storage_handlers.go:140-149`).
6. `append_text` has no write cap, no audit entry, and makes no backup
   (`fs_handlers.go:310-344`), unlike every other writer.
7. Zip directory entries are counted after the directory branch
   (`fs_handlers.go:795-807`), so entry-count flooding via directories is
   unbounded; a failed extraction leaves partial files.
8. `find_files`/`tree_directory` silently swallow walk errors; `file_exists`
   resolves only the parent directory.

**Audit:** JSONL at `<storage>/audit.jsonl`, 0600, no redaction, and coverage is
limited to `delete_file`, overwrite backups, and skipped-symlink counters.
Reads, plain writes, append, copy, move, mkdir, touch and zip operations are not
audited. A second, unrelated JSONL is written to `$TMPDIR/mcp-onlyoffice-audit.jsonl`
(`server/result.go:177-194`).

**Tests:** 10 traversal/absolute/NUL denial cases, symlink read escape, symlink
write/delete refusal, overwrite refusal plus backup plus audit, recursive-delete
gate, five zip cases, walk/write limits. Not covered: deleting or moving a root,
backup-path symlink, hardlink, append limits, import/export, zip directory
flooding, TOCTOU, overlapping roots.

## 8. OOXML engine

**VERIFIED.** `internal/ooxml` is a writer only. Every entry point builds a
fresh `map[string]string` of parts and zips it (`docx.go:380,425,489`,
`xlsx.go:51`, `pptx.go:23`). There is no read-modify-write path and therefore no
preservation of unknown or unmodelled parts: any existing DOCX/XLSX/PPTX that
passes through this code is regenerated.

Production capability:

- DOCX: content types, rels, core/app properties, document, styles (Normal,
  Title, Heading1-3, Quote, TableGrid), TOC field, PAGE footer, tables with
  `tblHeader` and banding, header/footer. No numbering, theme, fontTable,
  settings, comments, images, hyperlinks, charts, footnotes.
- XLSX: single sheet, shared strings, two fonts, one border, `#,##0.00` number
  format declared but unused, formulas written with a hardcoded `<v>0</v>`
  (`xlsx.go:186-188`). No views, merges, autofilter, named ranges, charts.
- PPTX: one blank master, one layout, one theme, explicit text boxes per slide,
  16:9. No notes, tables, images, charts, hyperlinks, transitions.

ZIP writes are unbounded (whole package in memory, no entry/size/ratio limits).
The validator (`validator/validator.go`) checks signature, required parts,
content-type substrings, XML well-formedness, relationship target existence, and
XLSX shared strings; it does not check ISO 29500 schema, element order,
relationship id uniqueness, `TargetMode`, content-type coverage for every part,
or compression ratio. Its own test helper claims relationship validation it does
not perform (`ooxml_test.go:315`).

No round-trip-modify test exists, no fixture from a real Office install, no
no-op preservation test.

## 9. Python engine

**VERIFIED good:** NDJSON over a persistent child process, monotonic IDs, 120 s
default timeout, stderr drained to debug logs (never to the client), read caps
(8 MiB request, 64 MiB response), kill-and-respawn on crash or malformed
response, capability allowlist with a two-segment regex, module/method whitelist
that rejects private and non-callable names, and argument confinement for keys
that look path-like. Pinned dependencies (openpyxl 3.1.5, python-docx 1.2.0,
python-pptx 1.0.2, matplotlib 3.11.0, numpy 2.5.2, fpdf2 2.7.0, PyPDF2 3.0.0,
Pillow 13.0.0).

**VERIFIED defects:**

- The child inherits the full parent environment; only
  `MCP_ONLYOFFICE_ENGINE_OUTPUT_DIR` is appended (`engine/bridge.go:173-175`).
  There is no scrubbing of unrelated secrets.
- A Python error reply without an `id` (oversized line, invalid JSON) is treated
  as an ID mismatch and kills the engine instead of being logged and ignored
  (`runner.py:111-122` versus `bridge.go:445-448`).
- Exit code is discarded (`bridge.go:202`), so a non-zero exit is only visible as
  a dead process.
- Duplicate response IDs are not detected; a duplicated ID would be accepted.
- Path confinement is heuristic: only string values under keys matching
  `path|output|dest|file|filename|image` are checked (`validator.py:20,78-101`),
  so keys like `src`, `template`, `doc` bypass it.
- No OS-level sandbox: no rlimits, no namespaces, no dropped user, network not
  blocked.

## 10. Tools and profiles

**VERIFIED.** 70 tools in the legacy profile, 16 in the professional profile;
professional is a filter over legacy, with zero professional-only tools. Every
active tool is registered at startup regardless of tier; tiers only feed
`discover_tools` and `load_tool_tier` (`server/discovery.go:57-72`), which
contradicts the progressive-loading claim in the README.

Tool annotations already exist for every legacy definition
(`TestAllLegacyDefinitionsHaveAnnotations`), and `professional_test.go` spot-checks
them. Annotations are informational only; no policy reads them.

Input schemas omit `overwrite` and `recursive`, although the handlers read both
(`fs_handlers.go:198,532`; `doc_handlers.go:88,167,193,218,262`). A client that
validates against the schema cannot overwrite or delete a directory.

`document_agent` documents `sections[].path`/`image` but the handler discards
them. `create_report` forwards paths to the Python engine, whose confinement root
is only the storage directory, so it fails when `allowed_roots` is wider.

No tool exposes raw JavaScript, macros, shell, or network to the model.
`os/exec` is used only for `python3 runner.py` and the Desktop binary with fixed
arguments. There is no `eval`-like dispatch anywhere in Go or Python.

## 11. Dependency vulnerability status

**NOT VERIFIED.** No `govulncheck`, no `osv-scanner`, no secret scan, no SBOM, no
license check configured or run. Research on the exact pinned versions is in
progress; results will be recorded when available. No claim of "no known
vulnerabilities" is made until that run exists.

## 12. Benchmark and evidence

**BLOCKED / absent.** No benchmark directory, corpus manifest, fixture set,
visual-regression harness, or metric collection exists. Every quality claim in
the README (formatting preservation, task success, compatibility) is therefore
**DECLARED**, not measured. No competitor comparison exists.

## 13. Documentation state

Present: `README.md` (25 KB), `CHANGELOG.md`, `docs/TOOL_PROFILES.md`, `LICENSE`.

Absent: `docs/architecture.md`, `docs/security.md`, `docs/threat-model.md`,
`docs/desktop-bridge.md`, `docs/backends.md`, `docs/transactions.md`,
`docs/benchmark.md`, `docs/prior-art.md`, `docs/compatibility.md`,
`SECURITY.md`, `CONTRIBUTING.md`.

README claims that contradict this audit and must be corrected or explicitly
labelled as planned:

- progressive tool loading (all tools are registered up front);
- working without Desktop (startup aborts without it);
- implied formatting fidelity for edits (the plugin only has six coarse
  commands, and OOXML output is generated from scratch);
- the word "tested"/"verified" attached to Desktop behaviour that was only
  exercised on one Linux machine, without reproducible evidence.

## 14. Ranked risk summary

| # | Risk | Severity | Phase |
|---|---|---|---|
| 1 | Entrypoint missing from git; clean clone does not build | Critical | A |
| 2 | Core aborts without Desktop or Python | High | A |
| 3 | `delete_file`/`move_file` can destroy an allowed root | High | A (guard) / B |
| 4 | Backup-path symlink writes outside roots | High | A |
| 5 | Single implicit editor session; no document identity, no revision, silent overwrite | High | B/C |
| 6 | Timeout after a mutation cannot distinguish success from failure | High | C |
| 7 | No CI, no vulnerability scan, no SBOM, no secret scan | Medium | A |
| 8 | OOXML regenerates instead of preserving; no preservation tests | Medium | D/E |
| 9 | Python inherits full environment; no OS sandbox | Medium | G |
| 10 | Unbounded reads in `ensureWritable`/`import_document`; `append_text` uncapped | Medium | A |
| 11 | Version strings drift across four files | Low | A |
| 12 | Dead code, triplicated helpers, contradictory docs | Low | A/B |
| 13 | No benchmark, corpus, or reproducible evidence for any quality claim | High (credibility) | F |

## 15. Adjusted plan

Phase A, in this order, each as a small reviewable commit:

1. Commit the entrypoint and the anchored `.gitignore`; verify a clean clone
   builds. No behaviour change.
2. Single version source; align server, bridge, plugin, and engine protocol
   version; add a consistency test.
3. Separate readiness: `core_ready`, `python_available`, `desktop_available`,
   `builder_available` (false until Phase E), `document_server_available`.
   Startup succeeds without Desktop and without Python; a missing optional
   backend degrades its own tools only. Add a `health` tool.
4. Fix the two filesystem defects that Phase A can fix without redesign: refuse
   to delete or move an allowed root, and refuse to write a backup through a
   symlink. Add regression tests.
5. CI on Linux, Windows, macOS: build, vet, test, race (Linux), gofmt, Python
   tests, vulnerability scan, secret scan, SBOM, license report, artifact build.
   Every check must be green before Phase B.

Then Phase B (bridge protocol v2, session registry, runtime pairing), Phase C
(typed operations, revisions, transactions, receipts), Phase D (granular
editing), Phase E (optional Document Builder), Phase F (benchmark), Phase G
(hardening: fuzz, malicious fixtures, resource limits), Phase H (distribution).

Nothing in this document is a claim about competitor behaviour, and no
"number one" status is asserted anywhere.
