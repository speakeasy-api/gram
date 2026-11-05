---
"server": patch
---

OpenClaw calls to a Speakeasy gateway now show as Gateway calls instead of Shadow MCP calls. OpenClaw names MCP tools `<server>__<tool>`, so a gateway configured as "speakeasy" looked like any third-party server of that name. The OpenClaw observability plugin now sends the name and URL of each configured MCP server on tool events, never its headers or credentials, and `speakeasy-hooks` reports the URL the call reached. Tool usage classifies calls to agent gateways (`/agent-mcp/<agent>`), the organization gateway and project gateways by that URL and names them after the agent, organization or project. A third-party server with the same name at another URL stays a shadow MCP server. The relay half takes effect with the next `speakeasy-hooks` release pin.
