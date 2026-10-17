---
"server": minor
"dashboard": minor
---

Tunneled MCP servers can now be permissioned by tool, like remote ones. The Inspect tab records the tools a tunneled server lists, and the role editor, grant drawer and agent grant picker offer those recorded tools for tool-level and annotation-level rules. Because a tunnel may list different tools for each user, a listing only ever adds tools: a stored tool's annotations are updated, or its metadata removed after confirmation, one tool at a time, never replaced in bulk. When the tunnel has no agent connected, the Inspect tab says it is offline and keeps showing the recorded tools, and the role editor says so for a server with no recorded tools.
