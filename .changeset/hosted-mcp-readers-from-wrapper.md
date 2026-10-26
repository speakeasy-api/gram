---
"server": patch
---

Hosted MCP servers now read their enabled, public and session-issuer state from the MCP server instead of the toolset: legacy `/mcp/<slug>` serving and install pages, gateway members, consent, plugins, assistants, access visibility, usage metrics, search indexing, the issuer attach preview and the deployment impact report. Today both copies agree, so nothing changes for users; a hosted server whose MCP server no longer matches its toolset is still not served.
