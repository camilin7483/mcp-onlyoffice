#!/usr/bin/env bash
set -euo pipefail

BINARY="mcp-onlyoffice"
BIN_DIR="${HOME}/.local/bin"
CONFIG_DIR="${HOME}/.config/mcp-onlyoffice"

if [ -f "${BIN_DIR}/${BINARY}" ]; then
    echo "[uninstall] Removing binary..."
    rm -f "${BIN_DIR}/${BINARY}"
else
    echo "[uninstall] Binary not found at ${BIN_DIR}/${BINARY}"
fi

if [ -d "${CONFIG_DIR}" ]; then
    echo "[uninstall] Removing config directory..."
    rm -rf "${CONFIG_DIR}"
fi

echo "[uninstall] Done."
echo "Storage directory (~/mcp-onlyoffice) was NOT removed."
echo "Remove it manually: rm -rf ~/mcp-onlyoffice"
