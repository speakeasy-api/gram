---
"server": patch
---

Apply shadow-MCP policy to Codex's built-in MCP resource tools. Codex reaches
MCP servers through three meta-tools — `list_mcp_resources`,
`list_mcp_resource_templates` and `read_mcp_resource` — that carry no `mcp__`
prefix and name their target in `tool_input.server`. The unified ingest
endpoint decides whether a call is an MCP call from resolved MCP data or an
MCP-shaped tool name, and neither recognizes these, so they were classified as
ordinary tool calls: the risk scan ran but the shadow-MCP policy never did. A
`block_all` policy therefore did not stop a Codex session from reading any MCP
server's resources, while the legacy Codex endpoint denied the same call. The
gate now recognizes them for the codex adapter, and a meta-tool whose server
cannot be read is denied rather than allowed — an unproven target is not an
absent one.
