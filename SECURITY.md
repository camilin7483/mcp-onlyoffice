# Security Policy

## Supported versions

| Version | Supported |
|---|---|
| 2.2.x | yes |
| < 2.2.0 | no (upgrade; no backported fixes) |

Only the latest minor release receives security fixes.

## Reporting a vulnerability

**Do not open a public issue.** Report privately with:

1. A description of the issue and the affected component or tool name.
2. Steps to reproduce (config, backend involved, minimal tool call).
3. The server version (`-version`) and platform.

Preferred channel: a private security advisory on the GitHub repository.
If that is unavailable, contact the maintainer privately and allow a
reasonable disclosure window before any public write-up. Reports are
acknowledged on a best-effort basis; there is no paid bug bounty.

## Scope

In scope (the MCP server, the bridge, the engine, the builder wrapper):

- `internal/server/` — tool handlers, argument parsing, path containment.
- `internal/plugin/` — bridge pairing, token handling, plugin installer.
- `internal/engine/` + `python_engine/` — Go↔Python IPC, capability
  confinement, output-dir confinement.
- `internal/builder/` — Document Builder wrapper (argv-only exec,
  path/size containment).
- `internal/ooxml/`, `internal/validator/`, `internal/storage/` —
  document parsing caps, structural validation, filesystem sandbox.
- `internal/operations/` — operation decode/registry/policy/revision rules.

Non-goals (explicitly out of scope):

- OS-level attackers (root, kernel, container escape).
- Compromise of ONLYOFFICE Desktop Editors itself.
- Network or multi-user deployment: the bridge binds loopback only and
  pairing assumes a single-user machine.
- Side channels (timing, cache) and audit-log tampering by a same-user
  attacker.
- Prompt injection via document text reaching the model as tool output.
- The trusted Document Builder binary itself (a trojaned binary equals user
  compromise).

## Reference

The full as-implemented threat model, with mitigations per threat and
residual risks, is `docs/threat-model.md`. The sandbox rules are
`docs/security.md`. Both mark unverifiable claims UNVERIFIED inline.
