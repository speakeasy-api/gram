---
"server": patch
---

Hosted MCP servers now take their enabled, public and session-issuer state from the MCP server everywhere it is read: legacy `/mcp/<slug>` serving and install pages, gateway members, consent, plugins, assistants, access visibility, usage metrics, search indexing and the deployment impact report. The serving path no longer rejects a hosted server whose toolset settings differ from its MCP server.
