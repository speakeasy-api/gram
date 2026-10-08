---
"server": minor
"dashboard": minor
---

Tunneled MCP servers can now be permissioned by tool, like remote ones. The Inspect tab records the tools a tunneled server lists, and the role editor, grant drawer and agent grant picker offer those recorded tools for tool-level and annotation-level rules. Because a tunnel may list different tools for each user, a listing only ever adds tools: stored annotations are updated or removed one tool at a time, after confirmation, never replaced in bulk. When the tunnel has no agent connected, the Inspect tab and the role editor say the tunnel is offline and keep showing the recorded tools.
