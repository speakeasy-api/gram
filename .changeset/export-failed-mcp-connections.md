---
"server": minor
"dashboard": minor
---

Add an "MCP connection logs" data export for organizations with early access. When enabled, failed MCP connections and server problems an admin or service provider needs to fix are sent to the project's OTEL destination as `speakeasy.mcp_connection` events. Blocked or declined connections and connections through organization gateways are not included.
