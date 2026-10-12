---
"server": patch
---

Hosted MCP server traffic now records the MCP server and endpoint it was served through on tool call and resource read logs, usage events, analytics events, request logs, and the tool call and request duration metrics.

Legacy hosted requests also attribute unambiguous server identities without changing authorization, and remote tool-call and resource-read usage events include their fronting server identity. Hosted tools-list analytics emits the shared `mcp_server_tools_list` event alongside the existing `mcp_server_count` event for compatibility; analytics consumers should select one event rather than sum both.
