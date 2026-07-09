---
"server": patch
---

fix(telemetry): keep deleted MCP servers' tool-usage classification. Tool-usage `target_type` now resolves against live + soft-deleted MCP servers, so a managed remote/tunneled server's history no longer flips to `shadow_mcp_server` once the server is deleted or recreated.
