---
"server": minor
---

Progressive gateways offer `execute_read_only_tool`, marked read-only, beside `execute_tool`. It runs only member tools annotated read-only and refuses any other tool, so MCP clients that skip approval for read-only tools can run those calls without a prompt.
