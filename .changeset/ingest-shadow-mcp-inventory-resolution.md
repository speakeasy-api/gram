---
"server": patch
---

Stop labelling hosted MCP servers reached through a claude.ai connector as
shadow MCP. Such a connector appears in no local config file, so the device
cannot resolve its URL and reports the tool call with a server name but no
transport. The unified ingest path then never consulted the MCP inventory the
same session already shipped at session start — the one place that URL exists —
so the call reached enforcement and telemetry with no server URL: the
shadow-MCP guard denied a sanctioned server on missing evidence rather than on
evidence of a shadow one, and the hook row landed without
`gram.mcp.server_url`, leaving it classified as a shadow MCP server. The
inventory is now cached per session and consulted when an event carries no
transport of its own.
