# Transactions: the operation lifecycle

Implemented in `internal/operations/` (schema, registry, policy, revision,
transactions) and wired to backends in
`internal/server/operations_handler.go` (`handleDocumentOperation`).
Only the six operations in `defaultOperationRegistry` exist; anything else
is rejected as unknown.

## Lifecycle, as implemented

1. **Decode** — `DecodeOperation` (`internal/operations/decode.go`):
   64 KiB request cap, `DisallowUnknownFields`, dotted capability-style name
   pattern, non-empty target, no paragraph+range mixing, non-negative
   indexes, at most 64 param keys matching `^[a-z_][a-z0-9_]{0,63}$`.
   Errors name the field and never echo param data.
2. **Registry** — `Registry.Lookup`
   (`internal/operations/registry.go`): the name must be registered with a
   known backend, a known risk level, and a non-empty capability.
   Unknown names fail with the list of available operations.
3. **Policy** — `Policy.Decide` (`internal/operations/registry.go`):
   `read_only` and `safe_mutation` are allowed; `sensitive_mutation`
   requires approval + diff unless pre-approved for the process;
   `destructive` always requires approval + diff;
   `network`/`admin` are denied by default.
4. **Revision check** — `CheckRevision`
   (`internal/operations/revision.go`): when the caller passes
   `expected_revision`, it must equal the current digest or a
   `ConflictError` aborts before any write. Both empty means no check.
5. **Dry run** — when `dry_run: true` and the spec supports it
   (`SupportsDryRun`; desktop `writer.*` ops do not), the edit is computed
   and validated but nothing is written; the receipt has status `dry_run`.
6. **Apply** — OOXML: read file (64 MiB cap), apply the in-place edit
   (`internal/ooxml/edit.go`), validate the new bytes in memory first.
   Desktop: send the command to the editor session (30 s context, 10 s
   per send) (`applyDesktopOperation`).
7. **Validate** — `validateBytes` runs the structural validator
   (`internal/validator/validator.go`) over the new package. If it fails,
   nothing is written and the caller gets an error (not a receipt).
8. **Receipt** — success commits the transaction and returns an `opReceipt`
   (schema below); desktop timeouts settle as `unknown_outcome` with no
   after digest and never report success.

## Transaction state machine

States (`internal/operations/transaction.go`):

```
prepared → committed → rolled_back
prepared → rolled_back
prepared → unknown_outcome
prepared → failed (implicit: handler errors before settlement)
```

- `Commit` is allowed from `prepared` only.
- `Rollback` is allowed from `prepared` or `committed`.
- `MarkUnknown` is allowed from `prepared` only (backend timeout: the write
  may or may not have happened).
- Terminal states (`committed`, `rolled_back`, `unknown_outcome`) accept no
  further transition; each returns an error naming the current status.

## `unknown_outcome`

Returned only when the desktop bridge reports an outcome-unknown RPC error
(killed/timed-out send — see `internal/plugin/errors.go` and the recovery
contract in `internal/server/health.go:requirePythonEngine` for the
equivalent engine rule). The receipt carries `unknown_outcome: true`, an
empty after digest, a diff placeholder pointing at the open document, and
`validated: false`. The caller must re-inspect the document; retrying blindly
may duplicate the edit.

## Rollback semantics: what is and is not reversible

- **Reversible in the registry is advisory.** `Spec.Reversible` marks the
  four `ooxml.*` ops reversible and the two `writer.*` ops not, but
  `Transaction.Rollback` only flips the in-memory status and returns a
  receipt with `rolled_back: true` — it does **not** restore file bytes.
- **What actually preserves data:** before overwriting, OOXML writes go
  through `prepareWrite`, which leaves a `.bak-<timestamp>` copy beside the
  file (`internal/server/operations_handler.go:applyOOXMLOperation`,
  `internal/server/fs_handlers.go:writeBackup`). Recovery is manual: copy
  the `.bak-` file back. There is no automatic restore path in the tree.
- **Desktop edits have no backup at all.** `writer.replace_text` /
  `writer.insert_text` mutate the open document; nothing snapshots the
  previous state, so a committed desktop edit cannot be rolled back by this
  server. The `Reversible: false` flag says exactly this.
- **Dry runs are the safe preview:** they validate without writing and are
  the only side-effect-free way to review an OOXML diff before committing.

## Receipt schema

`opReceipt` (`internal/server/operations_handler.go`):

```jsonc
{
  "transaction_id": "op-<hex>",
  "status": "committed | dry_run | unknown_outcome",
  "operation": "ooxml.docx.replace_paragraph_text",
  "document_id": "…",        // when the request carried one
  "before_digest": "…",      // hex digest before the edit
  "after_digest": "…",       // empty on unknown_outcome
  "revision": "…",           // current digest, for the next expected_revision
  "diff": {
    "changes": [{"kind": "replace|insert|delete|format",
                 "before": "…", "after": "…", "where": "paragraph 3"}],
    "summary": "replace paragraph 3: 41 chars -> 12 chars"
  },
  "validated": true,         // false on desktop receipts (no package to check)
  "unknown_outcome": true,   // only on unknown_outcome
  "matches": 2               // only writer.replace_text: SearchAndReplace count
}
```

`where` values never carry absolute paths. Digests are opaque hex strings
from `ooxml.Digest`; `revision` from one receipt feeds `expected_revision`
of the next to get optimistic-concurrency conflicts instead of silent
overwrites.
