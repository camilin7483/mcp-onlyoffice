#!/usr/bin/env bash
set -euo pipefail

BINARY="mcp-onlyoffice"
CONFIG_DIR="${HOME}/.config/mcp-onlyoffice"
STORAGE_DIR="${HOME}/mcp-onlyoffice/documents"
BIN_DIR="${HOME}/.local/bin"

echo "[install] Building ${BINARY}..."
go build -o "${BINARY}" ./cmd/mcp-onlyoffice

echo "[install] Creating directories..."
mkdir -p "${CONFIG_DIR}" "${STORAGE_DIR}" "${BIN_DIR}"

echo "[install] Installing binary..."
install -m 755 "${BINARY}" "${BIN_DIR}/${BINARY}"

if [ ! -f "${CONFIG_DIR}/config.json" ]; then
    echo "[install] Creating default config..."
    cat > "${CONFIG_DIR}/config.json" <<EOF
{
  "document_server_url": "http://localhost:8080",
  "callback_host": "http://localhost:2020",
  "callback_port": 2020,
  "storage_path": "${STORAGE_DIR}",
  "jwt_secret": "",
  "jwt_enabled": false,
  "log_level": "info"
}
EOF
fi

echo "[install] Done!"
echo "  Binary: ${BIN_DIR}/${BINARY}"
echo "  Config: ${CONFIG_DIR}/config.json"
echo "  Storage: ${STORAGE_DIR}"
echo ""
echo "Make sure ${BIN_DIR} is in your PATH."
echo "Add to ~/.config/opencode/opencode.json:"
echo ""
echo '{
  "mcpServers": {
    "mcp-onlyoffice": {
      "command": "'"${BIN_DIR}/${BINARY}"'",
      "args": ["-config", "'"${CONFIG_DIR}/config.json"'"]
    }
  }
}'
