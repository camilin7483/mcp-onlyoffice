# Project memory

## Release audit branch

- Linux Desktop Editors needs a recognized file extension for managed files.
  `LocalStore.DesktopPath` records the payload extension before moving the
  legacy file. An interrupted migration can still read the `.data` payload.
  Document identity and references are independent of the stored filename.
- Load the installed ONLYOFFICE SDK before advertising editor capabilities.
  The bridge sends commands without a session ID only when a single editor is
  connected. Filesystem revisions do not apply to in-memory editor sessions.
- The Linux interactive audit used ONLYOFFICE 9.0.4.50 and verified a managed
  DOCX round-trip through MCP and an editor save. See `docs/compatibility.md`
  for the scope of that verification.

## Follow-up verification

- An XLSX number-format change derives a style from the target cell. Numeric
  text with leading zeros or precision Excel cannot preserve stays text.
- Locks coordinate shared-store MCP mutations and typed edits. Desktop's
  open-for-edit marker is checked before writing, and typed edits compare
  the original file again after staging, immediately before replacing it.
- Payload replacement no longer depends on a second metadata write. Reads
  derive size and modification time from the stored file. Deletion saves
  the index before removing the payload, so an index failure preserves bytes.
- On Linux, XLSX formatting survived a real editor save and PPTX title
  edits persisted. Tests also ran two independent MCP servers against the
  same storage directory. Unsupported external writers remain uncoordinated.
