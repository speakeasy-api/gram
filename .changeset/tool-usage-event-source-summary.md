---
"server": patch
---

The MCP server & tool insights page and the Tool logs page load faster and no longer time out on long date ranges. Splitting skill loads by how they arrived (hook or MCP) now reads pre-aggregated trace data instead of scanning every raw log in the range.
