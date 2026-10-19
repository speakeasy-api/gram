---
"server": patch
---

Tool call and skill charts read a per-call table instead of re-sorting every event in the window, so dashboards like MCP & Tools load without running ClickHouse out of memory.
