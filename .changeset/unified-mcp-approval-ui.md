---
"dashboard": minor
---

Unify Shadow MCP server pages with the MCP approval flow. The server detail page absorbs the approval review (evidence, requesters, decision history, decide form) and every allow/deny travels one write path: a recorded approval decision, opened on the spot for servers with no pending request. The standalone allow-rule/block/unblock/bypass action sheets are retired, approval queue rows and command-palette results land on the server page, and request links for URL targets redirect there.
