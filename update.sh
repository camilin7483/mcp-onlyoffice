#!/usr/bin/env bash
set -euo pipefail

BINARY="mcp-onlyoffice"
BIN_DIR="${HOME}/.local/bin"
REPO_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "[update] Pulling latest changes..."
cd "${REPO_DIR}"
git pull --rebase 2>/dev/null || echo "  (not a git repository, skipping pull)"

echo "[update] Building..."
go build -o "${BINARY}" ./cmd/mcp-onlyoffice

echo "[update] Installing..."
install -m 755 "${BINARY}" "${BIN_DIR}/${BINARY}"

echo "[update] Done. Binary updated: ${BIN_DIR}/${BINARY}"
