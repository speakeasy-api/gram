---
"dashboard": patch
"server": patch
---

The Shadow MCP server table loads every page with "Load more". The control now sits below the last row instead of floating over the rows as you scroll, and new rows append in the server's order. The list endpoint is declared as cursor-paginated, so SDK clients get page iterators.
