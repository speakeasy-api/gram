---
"server": minor
---

Emit the MCP protocol version to OpenTelemetry on all five inbound MCP paths. Traces now carry `gram.mcp.requested_protocol_version` and `gram.mcp.negotiated_protocol_version`, and a new unsampled `mcp.initialize` counter breaks handshakes down by revision, so client version adoption can be measured and a version-specific failure can be diagnosed.
