---
"server": patch
---

MCP observability now keys on each request instead of the initialize handshake,
which the 2026-07-28 protocol revision removes: a new unsampled `mcp.request`
counter records every dispatched MCP request by protocol revision, method, and
surface across all serving paths, and both `tools/list` PostHog events carry
the client's protocol version, name, version, and capabilities. The method
label on `mcp.request.duration` is now clamped to a known set so clients cannot
mint unbounded series, and the `MCP-Protocol-Version` header no longer leaks
into tool environment variables as a `protocol_version` value.
