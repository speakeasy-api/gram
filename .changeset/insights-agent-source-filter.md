---
"dashboard": patch
---

Fix the Agent filter on the MCP & Tools insights page not reloading data. Selecting an agent updated the filter chip but the tool usage summary ignored the hook source, so the graphs never changed.
