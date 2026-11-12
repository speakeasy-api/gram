---
"server": minor
"dashboard": minor
---

Add an "MCP connection logs" data export. When enabled, failed MCP connections and server problems an admin or service provider needs to fix are sent to the project's OTEL destination as `speakeasy.mcp_connection` events. By default, blocked or declined connections are not included. Connections through organization gateways are not included.
