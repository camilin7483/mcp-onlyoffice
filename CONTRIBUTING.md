# Contributing

## Build / test / benchmark

```sh
make build        # build binary
make clean        # remove binary
make run          # build + run
make dev          # go run (no build)
go test ./... -count=1
go vet ./...
gofmt -l .        # must print nothing
make benchmark    # reproducibility benchmark (see docs/benchmark.md)
PATH="$(pwd)/.venv/bin:$PATH" python3 -m unittest discover -s python_engine
```

`make test` runs the Go suite plus the Python engine tests; `make lint`
gates gofmt and vet; `make ci` runs build + lint + test.

## The capability rule

**A capability needs a test before it is advertised.** Concretely:

- A new MCP tool needs a handler, a `Definitions()` entry
  (`internal/server/tools.go`), a tier registration
  (`internal/server/discovery.go`), and a contract/test entry proving it
  (see `internal/server/tool_contract_test.go`, which pins the 74-tool
  legacy surface — update the count when the surface changes).
- A new `document_operation` needs a registry spec
  (`internal/server/operations_handler.go:defaultOperationRegistry`),
  decode/policy coverage (`internal/operations/*_test.go`), and an
  apply-path test (`internal/server/operations_handler_test.go`).
- A new Python capability needs engine registration
  (`python_engine/__init__.py`) and a Python unit test.

Untested surface is not merged.

## Commit / PR expectations

- One responsibility per change; no unrelated refactors or drive-by
  rewrites.
- Commit messages name the subsystem (`plugin: …`, `ooxml: …`).
- PRs describe what was verified: commands run plus their tails.
  `go test ./... -count=1` must be all-green; new behavior needs a test.
- Do not claim verification that did not happen: live-editor runs,
  specific client apps, non-Linux platforms, and real-builder runs are
  UNVERIFIED unless the PR shows the evidence (see
  `docs/compatibility.md`).

## Formatting / lint gates

- `gofmt -l .` empty, `go vet ./...` empty — CI enforces both
  (`.github/workflows/ci.yml`).
- Go code comments in English; user-facing docs in this repo are in
  English. Keep new files in the module that owns the responsibility
  (no catch-all files, no new layer without a reason).

## Offline checks

The Go suite, vet, and gofmt need no network:

```sh
gofmt -l .
go vet ./...
go test ./... -count=1
python3 -c "import yaml;yaml.safe_load(open('.github/workflows/release.yml'))"
```

The Python engine tests need only the local virtualenv. Anything requiring
a download (Go module fetch on a cold cache, pip install, release
publishing) is not an offline check.
