package version

// Version is the single source of truth for the product version:
// the MCP server, the plugin manifest, and the bridge identity.
// Do not write a product version literal anywhere else; import this package.
const Version = "2.2.0"

// BridgeProtocol is the integer wire protocol version for the plugin bridge.
// It changes only when the Go/JS message shape changes.
const BridgeProtocol = 2

// EngineProtocol is the Python NDJSON protocol version.
// It is versioned separately from the product version.
const EngineProtocol = "1.0.0"
