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
server's resources, while the legacy Codex endpoint denied the same call.

The gate now recognizes them for the codex adapter, and the named server is
resolved against the session's MCP inventory so a Gram-hosted target is still
allowed and a denied one is named. A meta-tool whose server cannot be resolved
is denied rather than allowed — an unproven target is not an absent one.
Sessions now cache their MCP inventory on the ingest path under the same key
and TTL the legacy per-provider endpoints use.

Rolled out on client capability rather than deploy order: releases before this
one report no adapter version and no MCP inventory, so enforcing on them would
deny every meta-tool call — including reads of Gram-hosted servers that work
today. Those clients keep their current behavior and are counted in the logs
until they upgrade. A client that does report a version but no inventory has no
MCP servers configured, so a meta-tool call has nothing legitimate to target
and is denied.
